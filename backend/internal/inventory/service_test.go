package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRepo stands in for the Postgres repository so the service rules can be read without a database. Nothing in the running binary uses it.
type fakeRepo struct {
	reserveCalls int
	confirmCalls int
	stockCalls   int
	ttl          time.Duration
	lastBatch    int
	ctxErr       error
	reservations map[string]Reservation
	stock        Stock
	err          error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		reservations: map[string]Reservation{},
		stock:        Stock{ItemID: "item_4021", Total: 10},
	}
}

func (f *fakeRepo) Reserve(ctx context.Context, itemID, userID string, quantity int, ttl time.Duration) (Reservation, error) {
	f.reserveCalls++
	f.ttl = ttl
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return Reservation{}, f.err
	}
	res := Reservation{
		ID:        "res_" + userID,
		ItemID:    itemID,
		UserID:    userID,
		Quantity:  quantity,
		ExpiresAt: time.Now().Add(ttl).UTC(),
	}
	f.reservations[res.ID] = res
	return res, nil
}

func (f *fakeRepo) Confirm(ctx context.Context, reservationID string) (Reservation, error) {
	f.confirmCalls++
	f.ctxErr = ctx.Err()
	if f.err != nil {
		return Reservation{}, f.err
	}
	res, ok := f.reservations[reservationID]
	if !ok {
		return Reservation{}, ErrReservationNotFound
	}
	res.ConfirmedAt = time.Now().UTC()
	return res, nil
}

func (f *fakeRepo) Stock(ctx context.Context, itemID string) (Stock, error) {
	f.stockCalls++
	if f.err != nil {
		return Stock{}, f.err
	}
	if itemID != f.stock.ItemID {
		return Stock{}, ErrItemNotFound
	}
	return f.stock, nil
}

func (f *fakeRepo) ExpireDue(ctx context.Context, batch int) (int, error) {
	f.lastBatch = batch
	return 0, f.err
}

func (f *fakeRepo) Drifted(ctx context.Context) ([]string, error) {
	return nil, f.err
}

func newService(f *fakeRepo) *Service {
	return NewService(f, ServiceOptions{
		ReservationTTL: time.Minute,
		ReaperBatch:    10,
		WriteTimeout:   2 * time.Second,
	})
}

func TestReserveRejectsMissingReferences(t *testing.T) {
	cases := []struct {
		name      string
		itemID    string
		userID    string
		wantField string
	}{
		{"blank item id", "", "usr_9981", "item_id"},
		{"blank user id", "item_4021", "", "user_id"},
		{"item id over 64", strings.Repeat("x", 65), "usr_9981", "item_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRepo()
			_, err := newService(f).Reserve(context.Background(), tc.itemID, tc.userID, 1)

			var invalid *InvalidField
			if !errors.As(err, &invalid) {
				t.Fatalf("got %v, want an InvalidField", err)
			}
			if invalid.Field != tc.wantField {
				t.Errorf("field = %q, want %q", invalid.Field, tc.wantField)
			}
			if f.reserveCalls != 0 {
				t.Errorf("repository called %d times, want the request rejected first", f.reserveCalls)
			}
		})
	}
}

func TestReserveRejectsQuantityOutsideColumnRange(t *testing.T) {
	// 1<<31 is the first value postgres integer cannot hold.
	for _, quantity := range []int{0, -1, 1 << 31} {
		f := newFakeRepo()
		_, err := newService(f).Reserve(context.Background(), "item_4021", "usr_9981", quantity)

		var invalid *InvalidField
		if !errors.As(err, &invalid) {
			t.Fatalf("quantity %d: got %v, want an InvalidField", quantity, err)
		}
		if invalid.Field != "quantity" {
			t.Errorf("quantity %d: field = %q, want quantity", quantity, invalid.Field)
		}
		if f.reserveCalls != 0 {
			t.Errorf("quantity %d: repository was called anyway", quantity)
		}
	}
}

func TestReserveHandsTheConfiguredTTLToTheRepository(t *testing.T) {
	f := newFakeRepo()
	svc := NewService(f, ServiceOptions{ReservationTTL: 40 * time.Second, WriteTimeout: time.Second})

	if _, err := svc.Reserve(context.Background(), "item_4021", "usr_9981", 2); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if f.ttl != 40*time.Second {
		t.Errorf("ttl = %s, want 40s", f.ttl)
	}
}

// A client that hangs up must not roll back a reservation already accepted.
func TestWritePhaseIgnoresClientCancellation(t *testing.T) {
	f := newFakeRepo()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newService(f).Reserve(ctx, "item_4021", "usr_9981", 1); err != nil {
		t.Fatalf("reserve with a cancelled client: %v", err)
	}
	if f.ctxErr != nil {
		t.Errorf("write context died with the client: %v", f.ctxErr)
	}
}

func TestStockReadUsesTheRequestContextAndKeepsRepositoryErrors(t *testing.T) {
	f := newFakeRepo()
	_, err := newService(f).Stock(context.Background(), "item_ghost")
	if !errors.Is(err, ErrItemNotFound) {
		t.Errorf("got %v, want ErrItemNotFound", err)
	}
	if f.stockCalls != 1 {
		t.Errorf("stock calls = %d, want 1", f.stockCalls)
	}

	f = newFakeRepo()
	f.err = ErrInsufficientStock
	if _, err := newService(f).Reserve(context.Background(), "item_4021", "usr_9981", 99); !errors.Is(err, ErrInsufficientStock) {
		t.Errorf("got %v, want the repository error untouched", err)
	}
}

func TestConfirmRejectsBlankReservationID(t *testing.T) {
	f := newFakeRepo()
	_, err := newService(f).Confirm(context.Background(), "")

	var invalid *InvalidField
	if !errors.As(err, &invalid) {
		t.Fatalf("got %v, want an InvalidField", err)
	}
	if f.confirmCalls != 0 {
		t.Error("repository called for a blank id")
	}
}

func TestReaperBatchSizeComesFromOptions(t *testing.T) {
	f := newFakeRepo()
	svc := NewService(f, ServiceOptions{ReaperBatch: 250, WriteTimeout: time.Second})

	if _, err := svc.ExpireDueReservations(context.Background()); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if f.lastBatch != 250 {
		t.Errorf("batch = %d, want 250", f.lastBatch)
	}
}

func TestAvailableStockIsTotalMinusReserved(t *testing.T) {
	s := Stock{ItemID: "item_4021", Total: 100, Reserved: 15}
	if s.Available() != 85 {
		t.Errorf("available = %d, want 85", s.Available())
	}
}
