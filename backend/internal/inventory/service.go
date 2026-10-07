package inventory

import (
	"context"
	"time"
)

type ServiceOptions struct {
	ReservationTTL time.Duration
	ReaperBatch    int
	WriteTimeout   time.Duration
}

type Service struct {
	repo Repository
	opts ServiceOptions
}

func NewService(repo Repository, opts ServiceOptions) *Service {
	return &Service{repo: repo, opts: opts}
}

// Write-phase contexts detach from request cancellation: a client that closes
// its connection must not roll back a reservation the database already made.
func (s *Service) writeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.opts.WriteTimeout)
}

func (s *Service) Reserve(ctx context.Context, itemID, userID string, quantity int) (Reservation, error) {
	if err := validateRef("item_id", itemID); err != nil {
		return Reservation{}, err
	}
	if err := validateRef("user_id", userID); err != nil {
		return Reservation{}, err
	}
	if quantity < 1 {
		return Reservation{}, &InvalidField{Field: "quantity", Reason: "must be at least 1"}
	}
	if quantity > 1<<31-1 {
		return Reservation{}, &InvalidField{Field: "quantity", Reason: "exceeds the stock column range"}
	}

	writeCtx, cancel := s.writeContext(ctx)
	defer cancel()
	return s.repo.Reserve(writeCtx, itemID, userID, quantity, s.opts.ReservationTTL)
}

func (s *Service) Confirm(ctx context.Context, reservationID string) (Reservation, error) {
	if err := validateRef("reservation_id", reservationID); err != nil {
		return Reservation{}, err
	}
	writeCtx, cancel := s.writeContext(ctx)
	defer cancel()
	return s.repo.Confirm(writeCtx, reservationID)
}

func (s *Service) Stock(ctx context.Context, itemID string) (Stock, error) {
	if err := validateRef("item_id", itemID); err != nil {
		return Stock{}, err
	}
	return s.repo.Stock(ctx, itemID)
}

func (s *Service) ExpireDueReservations(ctx context.Context) (int, error) {
	return s.repo.ExpireDue(ctx, s.opts.ReaperBatch)
}

func (s *Service) Reconcile(ctx context.Context) ([]string, error) {
	return s.repo.Drifted(ctx)
}

func validateRef(field, value string) error {
	if value == "" {
		return &InvalidField{Field: field, Reason: "is required"}
	}
	if len(value) > 64 {
		return &InvalidField{Field: field, Reason: "must not exceed 64 characters"}
	}
	return nil
}
