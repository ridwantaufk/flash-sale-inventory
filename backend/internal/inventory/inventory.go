package inventory

import (
	"errors"
	"time"
)

type Reservation struct {
	ID          string
	ItemID      string
	UserID      string
	Quantity    int
	ExpiresAt   time.Time
	ConfirmedAt time.Time
}

type Stock struct {
	ItemID   string
	Total    int
	Reserved int
}

func (s Stock) Available() int { return s.Total - s.Reserved }

var (
	ErrItemNotFound        = errors.New("item not found")
	ErrInsufficientStock   = errors.New("insufficient stock")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrAlreadyConfirmed    = errors.New("reservation already confirmed")
	ErrReservationExpired  = errors.New("reservation expired")
	ErrStockInvariant      = errors.New("reserved stock does not cover the reservation")
)

// InvalidFieldis a client error carrying enough detail to point at the field
type InvalidField struct {
	Field  string
	Reason string
}

func (e *InvalidField) Error() string { return e.Field + ": " + e.Reason }
