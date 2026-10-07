package inventory

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReserveConfirmLifecycleInPostgres(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 100)

	res, err := repo.Reserve(t.Context(), itemID, "usr_9981", 2, time.Minute)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if res.ID == "" || res.ExpiresAt.IsZero() {
		t.Fatalf("reservation came back incomplete: %+v", res)
	}

	if _, reserved := readStock(t, db, itemID); reserved != 2 {
		t.Fatalf("reserved after hold = %d, want 2", reserved)
	}

	if _, err := repo.Confirm(t.Context(), res.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	total, reserved := readStock(t, db, itemID)
	if total != 98 || reserved != 0 {
		t.Errorf("after confirm: total = %d reserved = %d, want 98 and 0", total, reserved)
	}
}

// The $2 > 0 predicate lives in the statement because the reservations CHECK
// only constrains the row being inserted, not the arithmetic on inventory.
func TestNegativeQuantityCannotDonateStockBack(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 10)

	_, err := repo.Reserve(t.Context(), itemID, "usr_9981", -5, time.Minute)
	if err == nil {
		t.Fatal("a negative quantity was accepted")
	}
	if !errors.Is(err, ErrInsufficientStock) {
		t.Errorf("got %v, want ErrInsufficientStock", err)
	}

	total, reserved := readStock(t, db, itemID)
	if total != 10 || reserved != 0 {
		t.Errorf("stock moved: total = %d reserved = %d, want 10 and 0", total, reserved)
	}
}

func TestUnknownItemIsNotReportedAsSoldOut(t *testing.T) {
	repo, db := openTestRepository(t)
	seedItem(t, db, 0)

	if _, err := repo.Reserve(t.Context(), "itest_does_not_exist", "usr_9981", 1, time.Minute); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("got %v, want ErrItemNotFound", err)
	}
}

func TestExhaustedItemRefusesFurtherHolds(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 5)

	for i := 0; i < 5; i++ {
		if _, err := repo.Reserve(t.Context(), itemID, "usr_9981", 1, time.Minute); err != nil {
			t.Fatalf("hold %d: %v", i+1, err)
		}
	}
	if _, err := repo.Reserve(t.Context(), itemID, "usr_9981", 1, time.Minute); !errors.Is(err, ErrInsufficientStock) {
		t.Errorf("got %v, want ErrInsufficientStock", err)
	}

	if total, reserved := readStock(t, db, itemID); total != 5 || reserved != 5 {
		t.Errorf("total = %d reserved = %d, want 5 and 5", total, reserved)
	}
}

func TestExpiredReservationsGoBackToAvailableStock(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 7)

	res, err := repo.Reserve(t.Context(), itemID, "usr_9981", 3, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	expired, err := repo.ExpireDue(t.Context(), 50)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if expired == 0 {
		t.Fatal("nothing was swept although the deadline passed")
	}

	if total, reserved := readStock(t, db, itemID); total != 7 || reserved != 0 {
		t.Errorf("total = %d reserved = %d, want 7 and 0", total, reserved)
	}
	if _, err := repo.Confirm(t.Context(), res.ID); !errors.Is(err, ErrReservationExpired) {
		t.Errorf("got %v, want ErrReservationExpired", err)
	}
}

func TestConfirmingTwiceIsRefused(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 9)

	res, err := repo.Reserve(t.Context(), itemID, "usr_9981", 4, time.Minute)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := repo.Confirm(t.Context(), res.ID); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	if _, err := repo.Confirm(t.Context(), res.ID); !errors.Is(err, ErrAlreadyConfirmed) {
		t.Errorf("got %v, want ErrAlreadyConfirmed", err)
	}

	// One purchase, one decrement: the second confirm must not take stock again.
	if total, _ := readStock(t, db, itemID); total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

func TestConfirmUnknownReservationID(t *testing.T) {
	repo, db := openTestRepository(t)
	seedItem(t, db, 1)

	if _, err := repo.Confirm(t.Context(), "res_never_existed"); !errors.Is(err, ErrReservationNotFound) {
		t.Errorf("got %v, want ErrReservationNotFound", err)
	}
}

func TestConfirmAndExpiryRaceForTheSameReservation(t *testing.T) {
	repo, db := openTestRepository(t)
	itemID := seedItem(t, db, 20)

	res, err := repo.Reserve(t.Context(), itemID, "usr_9981", 5, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)

	var confirmErr error
	go func() {
		defer wg.Done()
		_, confirmErr = repo.Confirm(t.Context(), res.ID)
	}()

	var swept int
	go func() {
		defer wg.Done()
		swept, _ = repo.ExpireDue(t.Context(), 50)
	}()
	wg.Wait()

	total, reserved := readStock(t, db, itemID)

	confirmed := confirmErr == nil
	if confirmed && swept > 0 {
		t.Fatalf("both paths took effect: confirm succeeded and the reaper swept %d", swept)
	}
	if confirmed {
		if total != 15 || reserved != 0 {
			t.Errorf("confirmed: total = %d reserved = %d, want 15 and 0", total, reserved)
		}
	} else {
		if !errors.Is(confirmErr, ErrReservationExpired) {
			t.Fatalf("confirm failed with %v, want ErrReservationExpired", confirmErr)
		}
		if total != 20 || reserved != 0 {
			t.Errorf("expired: total = %d reserved = %d, want 20 and 0", total, reserved)
		}
	}

	drifted, err := repo.Drifted(t.Context())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(drifted) != 0 {
		t.Errorf("reserved_stock drifted for %v", drifted)
	}
}
