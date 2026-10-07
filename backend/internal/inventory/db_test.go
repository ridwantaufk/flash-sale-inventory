package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ridwantaufk/flash-sale-inventory/internal/platform/pg"
)

// Everything here talks to a real Postgres: the guarantee the assignment asks
// for is that no combination of concurrent writes can oversell, and that is a
// property of the database, not of the Go code around it.
//
// Set DATABASE_URL, or bring the compose stack up, and these run. Without one
// they skip loudly; the unit and contract tests above never need a database.
const defaultTestDatabaseURL = "postgres://flashsale:flashsale@localhost:5433/flashsale?sslmode=disable"

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = defaultTestDatabaseURL
	}

	db, err := pg.Open(t.Context(), url, pg.Options{MaxConns: 8, StatementTimeout: 5 * time.Second})
	if err != nil {
		t.Skipf("no postgres at %s: %v", url, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func openTestRepository(t *testing.T) (Repository, *sql.DB) {
	t.Helper()
	db := openTestDB(t)
	return NewPostgresRepository(db, quietLog()), db
}

// seedItem creates a throwaway row so concurrent tests never fight over the
// fixed catalogue, and removes it again whatever the outcome.
func seedItem(t *testing.T, db *sql.DB, total int) string {
	t.Helper()

	itemID := fmt.Sprintf("itest_%d_%d", time.Now().UnixNano(), total)
	if _, err := db.ExecContext(t.Context(),
		"INSERT INTO inventory (item_id, total_stock) VALUES ($1, $2)", itemID, total); err != nil {
		t.Fatalf("seed %s: %v", itemID, err)
	}

	t.Cleanup(func() {
		// The test context is already dead by the time cleanups run, so it cannot
		// be used to delete the rows we just wrote.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if _, err := db.ExecContext(ctx, "DELETE FROM reservations WHERE item_id = $1", itemID); err != nil {
			t.Errorf("cleanup reservations for %s: %v", itemID, err)
		}
		if _, err := db.ExecContext(ctx, "DELETE FROM inventory WHERE item_id = $1", itemID); err != nil {
			t.Errorf("cleanup inventory for %s: %v", itemID, err)
		}
	})

	return itemID
}

func readStock(t *testing.T, db *sql.DB, itemID string) (total, reserved int) {
	t.Helper()
	if err := db.QueryRowContext(t.Context(),
		"SELECT total_stock, reserved_stock FROM inventory WHERE item_id = $1", itemID,
	).Scan(&total, &reserved); err != nil {
		t.Fatalf("read stock for %s: %v", itemID, err)
	}
	return total, reserved
}

func reservationStatus(t *testing.T, db *sql.DB, reservationID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(t.Context(),
		"SELECT status FROM reservations WHERE reservation_id = $1", reservationID,
	).Scan(&status); err != nil {
		t.Fatalf("read status for %s: %v", reservationID, err)
	}
	return status
}
