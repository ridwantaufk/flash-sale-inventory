package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Repository interface {
	Reserve(ctx context.Context, itemID, userID string, quantity int, ttl time.Duration) (Reservation, error)
	Confirm(ctx context.Context, reservationID string) (Reservation, error)
	Stock(ctx context.Context, itemID string) (Stock, error)
	ExpireDue(ctx context.Context, batch int) (int, error)
	Drifted(ctx context.Context) ([]string, error)
}

type postgresRepository struct {
	db  *sql.DB
	log *slog.Logger
}

func NewPostgresRepository(db *sql.DB, log *slog.Logger) Repository {
	return &postgresRepository{db: db, log: log}
}

/* $2 > 0 lives in SQL because the reservations CHECK guards the row being inserted, not this arithmetic: a negative quantity would add stock back */
const claimSQL = `
    UPDATE inventory
       SET reserved_stock = reserved_stock + $2,
           updated_at     = now()
     WHERE item_id = $1
       AND $2 > 0
       AND total_stock - reserved_stock >= $2
    RETURNING reserved_stock`

const insertReservationSQL = `
    INSERT INTO reservations (reservation_id, item_id, user_id, quantity, expires_at)
    VALUES ('res_' || nextval('reservation_id_seq'), $1, $2, $3, now() + make_interval(secs => $4))
    RETURNING reservation_id, expires_at`

const confirmSQL = `
    UPDATE reservations
       SET status = 'confirmed', confirmed_at = now()
     WHERE reservation_id = $1
       AND status = 'active'
       AND expires_at > now()
    RETURNING item_id, quantity, confirmed_at`

const decrementSQL = `
    UPDATE inventory
       SET total_stock    = total_stock - $2,
           reserved_stock = reserved_stock - $2,
           updated_at     = now()
     WHERE item_id = $1
       AND $2 > 0
       AND reserved_stock >= $2
    RETURNING total_stock`

const stockSQL = `
    SELECT item_id, total_stock, reserved_stock
      FROM inventory
     WHERE item_id = $1`

func (r *postgresRepository) Reserve(ctx context.Context, itemID, userID string, quantity int, ttl time.Duration) (Reservation, error) {
	var out Reservation
	err := r.inTransaction(ctx, func(tx *sql.Tx) error {
		var reservedAfter int
		row := tx.QueryRowContext(ctx, claimSQL, itemID, quantity)
		switch err := row.Scan(&reservedAfter); {
		case errors.Is(err, sql.ErrNoRows):
			// zero rows is either "no such item" or "no such item left"
			// the client needs different codes, so ask which one it was.
			var exists bool
			if err := tx.QueryRowContext(ctx,
				"SELECT EXISTS(SELECT 1 FROM inventory WHERE item_id = $1)", itemID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrItemNotFound
			}
			return ErrInsufficientStock
		case err != nil:
			return err
		}

		res := Reservation{ItemID: itemID, UserID: userID, Quantity: quantity}
		if err := tx.QueryRowContext(ctx, insertReservationSQL,
			itemID, userID, quantity, int(ttl.Seconds())).Scan(&res.ID, &res.ExpiresAt); err != nil {
			return err
		}
		out = res
		return nil
	})
	return out, err
}

func (r *postgresRepository) Confirm(ctx context.Context, reservationID string) (Reservation, error) {
	var out Reservation
	err := r.inTransaction(ctx, func(tx *sql.Tx) error {
		var itemID string
		var quantity int
		var confirmedAt time.Time
		row := tx.QueryRowContext(ctx, confirmSQL, reservationID)
		switch err := row.Scan(&itemID, &quantity, &confirmedAt); {
		case errors.Is(err, sql.ErrNoRows):
			return classifyMiss(ctx, tx, reservationID)
		case err != nil:
			return err
		}

		var totalAfter int
		err := tx.QueryRowContext(ctx, decrementSQL, itemID, quantity).Scan(&totalAfter)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: item %s holds %d units", ErrStockInvariant, itemID, quantity)
		}
		if err != nil {
			return fmt.Errorf("decrement inventory: %w", err)
		}
		out = Reservation{ID: reservationID, ItemID: itemID, Quantity: quantity, ConfirmedAt: confirmedAt}
		return nil
	})
	return out, err
}

func classifyMiss(ctx context.Context, tx *sql.Tx, reservationID string) error {
	var status string
	var expiresAt time.Time
	err := tx.QueryRowContext(ctx,
		"SELECT status, expires_at FROM reservations WHERE reservation_id = $1", reservationID,
	).Scan(&status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReservationNotFound
	}
	if err != nil {
		return err
	}
	switch {
	case status == "confirmed":
		return ErrAlreadyConfirmed
	case status == "expired" || !time.Now().Before(expiresAt):
		return ErrReservationExpired
	default:
		return ErrReservationNotFound
	}
}

func (r *postgresRepository) Stock(ctx context.Context, itemID string) (Stock, error) {
	var s Stock
	err := r.db.QueryRowContext(ctx, stockSQL, itemID).Scan(&s.ItemID, &s.Total, &s.Reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrItemNotFound
	}
	return s, err
}

func (r *postgresRepository) ExpireDue(ctx context.Context, batch int) (int, error) {
	expired := 0
	err := r.inTransaction(ctx, func(tx *sql.Tx) error {
		// One sweep per tick no matter how many replicas are running.
		var locked bool
		if err := tx.QueryRowContext(ctx,
			"SELECT pg_try_advisory_xact_lock(hashtext('inventory_reaper'))").Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return nil
		}

		type due struct {
			id, itemID string
			qty        int
		}
		var pending []due
		rows, err := tx.QueryContext(ctx, `
    SELECT reservation_id, item_id, quantity
      FROM reservations
     WHERE status = 'active' AND expires_at <= now()
     ORDER BY expires_at
     FOR UPDATE SKIP LOCKED
     LIMIT $1`, batch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.id, &d.itemID, &d.qty); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, d)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		for _, d := range pending {
			// status='active' here is what stops a double decrement
			// when a confirm commits against the same row first
			res, err := tx.ExecContext(ctx, `
    UPDATE reservations SET status = 'expired'
     WHERE reservation_id = $1 AND status = 'active'`, d.id)
			if err != nil {
				return err
			}
			won, _ := res.RowsAffected()
			if won == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
    UPDATE inventory SET reserved_stock = reserved_stock - $2, updated_at = now()
     WHERE item_id = $1 AND reserved_stock >= $2`, d.itemID, d.qty); err != nil {
				return err
			}
			expired++
		}
		return nil
	})
	return expired, err
}

func (r *postgresRepository) Drifted(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT item_id FROM live_reservation_totals WHERE reserved_stock <> live_reserved")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func retryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

func (r *postgresRepository) inTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	const attempts = 5
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			backoff := time.Duration(2<<uint(i))*time.Millisecond +
				time.Duration(rand.Intn(2000))*time.Microsecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		err = r.runOnce(ctx, fn)
		if err == nil || !retryable(err) {
			return err
		}
		r.log.Debug("retrying transaction", "attempt", i+1, "error", err)
	}
	return fmt.Errorf("after %d attempts: %w", attempts, err)
}

func (r *postgresRepository) runOnce(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}
