package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

// The assignment's promise is behavioural rather than structural: under a burst
// of simultaneous reservations nobody may hold more units than exist, and a
// confirm racing the expiry sweep may not spend the same units twice. So these
// tests hammer the real endpoints against the real database, then ask the
// database whether the books balance.

type reserveOutcome struct {
	status int
	code   string
	id     string
}

func newPostgresRouter(t *testing.T, repo Repository, ttl time.Duration) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := NewHandler(NewService(repo, ServiceOptions{
		ReservationTTL: ttl,
		ReaperBatch:    500,
		WriteTimeout:   10 * time.Second,
	}), quietLog())

	engine := gin.New()
	engine.Use(httpx.RequestID())
	v1 := engine.Group("/api/v1/inventory")
	v1.POST("/reserve", h.Reserve)
	v1.POST("/confirm", h.Confirm)
	v1.GET("/stock", h.Stock)
	return engine
}

func burst(workers int, fn func(int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func reserveOnce(t *testing.T, engine *gin.Engine, itemID, userID string, quantity int) reserveOutcome {
	t.Helper()

	body := fmt.Sprintf(`{"user_id":%q,"item_id":%q,"quantity":%d}`, userID, itemID, quantity)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/reserve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	var fields struct {
		ReservationID string `json:"reservation_id"`
		Code          string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Errorf("reserve reply is not json: %v (%s)", err, w.Body)
	}
	return reserveOutcome{status: w.Code, code: fields.Code, id: fields.ReservationID}
}

func confirmOnce(t *testing.T, engine *gin.Engine, reservationID string) int {
	t.Helper()

	body := `{"reservation_id":` + fmt.Sprintf("%q", reservationID) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inventory/confirm", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code
}

func bookedUnits(t *testing.T, db *sql.DB, itemID string, statuses ...string) int {
	t.Helper()

	placeholders := make([]string, len(statuses))
	args := make([]any, 0, len(statuses)+1)
	args = append(args, itemID)
	for i, status := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, status)
	}

	var sum int
	query := fmt.Sprintf(
		"SELECT coalesce(sum(quantity), 0) FROM reservations WHERE item_id = $1 AND status IN (%s)",
		strings.Join(placeholders, ", "))
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&sum); err != nil {
		t.Fatalf("sum booked units for %s: %v", itemID, err)
	}
	return sum
}

func driftedItems(t *testing.T, db *sql.DB, itemID string) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `
    SELECT item_id FROM live_reservation_totals
     WHERE item_id = $1 AND reserved_stock <> live_reserved`, itemID)
	if err != nil {
		t.Fatalf("reconcile %s: %v", itemID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("reconcile %s: %v", itemID, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestConcurrentReservesNeverOversell(t *testing.T) {
	repo, db := openTestRepository(t)

	const (
		initial  = 100
		workers  = 60
		quantity = 2
		movable  = initial / quantity
	)
	itemID := seedItem(t, db, initial)
	engine := newPostgresRouter(t, repo, 5*time.Minute)

	var (
		mu       sync.Mutex
		outcomes []reserveOutcome
	)
	burst(workers, func(i int) {
		got := reserveOnce(t, engine, itemID, fmt.Sprintf("usr_%03d", i), quantity)
		mu.Lock()
		outcomes = append(outcomes, got)
		mu.Unlock()
	})

	if len(outcomes) != workers {
		t.Fatalf("recorded %d replies, want %d", len(outcomes), workers)
	}
	accepted, refused := 0, 0
	for _, o := range outcomes {
		switch {
		case o.status == http.StatusCreated:
			if !strings.HasPrefix(o.id, "res_") {
				t.Fatalf("reservation accepted without a usable id: %+v", o)
			}
			accepted++
		case o.status == http.StatusConflict && o.code == "INSUFFICIENT_STOCK":
			refused++
		case o.status >= 500:
			t.Errorf("server error under load: %+v", o)
		default:
			t.Errorf("unexpected reply: %+v", o)
		}
	}

	if accepted != movable {
		t.Errorf("accepted %d reservations of %d units, want %d", accepted, quantity, movable)
	}
	if refused != workers-accepted {
		t.Errorf("%d callers got a wrong refusal, want %d", refused, workers-accepted)
	}

	held := bookedUnits(t, db, itemID, "active", "confirmed")
	if held > initial {
		t.Errorf("oversold: reservations hold %d units of %d", held, initial)
	}
	if held != accepted*quantity {
		t.Errorf("reservations hold %d units, want %d", held, accepted*quantity)
	}

	total, reserved := readStock(t, db, itemID)
	if reserved != held {
		t.Errorf("reserved_stock is %d, the reservation rows say %d", reserved, held)
	}
	if total-reserved < 0 {
		t.Errorf("available stock went negative: total %d reserved %d", total, reserved)
	}
	if drift := driftedItems(t, db, itemID); len(drift) > 0 {
		t.Errorf("counters disagree with the reservation rows for %v", drift)
	}
}

// Confirm and the expiry sweep both want the same held units. The reservation
// row itself picks the winner, so the loser must give up quietly rather than
// spending the stock a second time.
func TestConfirmRacingExpirySpendsEachUnitOnce(t *testing.T) {
	repo, db := openTestRepository(t)

	const (
		initial = 40
		holders = 20
		ttl     = 200 * time.Millisecond
	)
	itemID := seedItem(t, db, initial)
	engine := newPostgresRouter(t, repo, ttl)

	ids := make([]string, holders)
	for i := range holders {
		got := reserveOnce(t, engine, itemID, fmt.Sprintf("usr_%02d", i), 1)
		if got.status != http.StatusCreated {
			t.Fatalf("reservation %d failed: %+v", i, got)
		}
		ids[i] = got.id
	}

	time.Sleep(ttl / 2)
	sweepUntil := time.Now().Add(4 * ttl)

	burst(holders+4, func(i int) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if i < holders {
			if status := confirmOnce(t, engine, ids[i]); status >= 500 {
				t.Errorf("confirm of %s returned %d", ids[i], status)
			}
			return
		}
		for time.Now().Before(sweepUntil) {
			if _, err := repo.ExpireDue(ctx, 500); err != nil {
				t.Errorf("expiry sweep: %v", err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := repo.ExpireDue(ctx, 500); err != nil {
		t.Fatalf("final sweep: %v", err)
	}

	confirmed := bookedUnits(t, db, itemID, "confirmed")
	expired := bookedUnits(t, db, itemID, "expired")
	if active := bookedUnits(t, db, itemID, "active"); active != 0 {
		t.Errorf("%d units are still active after every deadline passed", active)
	}
	if confirmed+expired != holders {
		t.Errorf("confirmed %d + expired %d = %d, want the %d held units counted once",
			confirmed, expired, confirmed+expired, holders)
	}

	total, reserved := readStock(t, db, itemID)
	if total != initial-confirmed {
		t.Errorf("total stock is %d, want %d: only the %d confirmed units may leave",
			total, initial-confirmed, confirmed)
	}
	if reserved != 0 {
		t.Errorf("reserved_stock is %d, want nothing held once every deadline passed", reserved)
	}
	if drift := driftedItems(t, db, itemID); len(drift) > 0 {
		t.Errorf("double spend left drift on %v", drift)
	}
}
