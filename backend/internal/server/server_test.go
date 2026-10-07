package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ridwantaufk/flash-sale-inventory/internal/config"
	"github.com/ridwantaufk/flash-sale-inventory/internal/inventory"
	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

type stubRepo struct{}

func (stubRepo) Reserve(context.Context, string, string, int, time.Duration) (inventory.Reservation, error) {
	return inventory.Reservation{
		ID:        "res_883291",
		ItemID:    "item_4021",
		UserID:    "usr_9981",
		Quantity:  2,
		ExpiresAt: time.Date(2026, 7, 20, 16, 35, 0, 0, time.UTC),
	}, nil
}

func (stubRepo) Confirm(context.Context, string) (inventory.Reservation, error) {
	return inventory.Reservation{
		ID:          "res_883291",
		ItemID:      "item_4021",
		Quantity:    2,
		ConfirmedAt: time.Date(2026, 7, 20, 16, 32, 0, 0, time.UTC),
	}, nil
}

func (stubRepo) Stock(context.Context, string) (inventory.Stock, error) {
	return inventory.Stock{ItemID: "item_4021", Total: 100, Reserved: 15}, nil
}

func (stubRepo) ExpireDue(context.Context, int) (int, error) { return 0, nil }

func (stubRepo) Drifted(context.Context) ([]string, error) { return nil, nil }

func newEngine(live DependencyCheck) (http.Handler, *httpx.Readiness) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := inventory.NewService(stubRepo{}, inventory.ServiceOptions{
		ReservationTTL: 5 * time.Minute,
		ReaperBatch:    500,
		WriteTimeout:   time.Second,
	})
	ready := httpx.NewReadiness()
	cfg := config.Config{GinMode: "test", HTTPAddr: ":0"}
	return NewEngine(cfg, inventory.NewHandler(svc, log), ready, live), ready
}

func do(t *testing.T, engine http.Handler, method, path, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestRoutesMatchTheBrief(t *testing.T) {
	engine, _ := newEngine(func(context.Context) error { return nil })

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/v1/inventory/reserve",
			`{"user_id":"usr_9981","item_id":"item_4021","quantity":2}`, http.StatusCreated},
		{http.MethodPost, "/api/v1/inventory/confirm", `{"reservation_id":"res_883291"}`, http.StatusOK},
		{http.MethodGet, "/api/v1/inventory/stock?item_id=item_4021", "", http.StatusOK},
		{http.MethodGet, "/api/v1/inventory/stock", "", http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/v1/inventory/reservation", `{}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		status, _ := do(t, engine, tc.method, tc.path, tc.body)
		if status != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, status, tc.want)
		}
	}
}

func TestHealthzTracksTheDatabase(t *testing.T) {
	engine, _ := newEngine(func(context.Context) error { return nil })
	if status, body := do(t, engine, http.MethodGet, "/healthz", ""); status != http.StatusOK {
		t.Errorf("with a reachable database: %d %s", status, body)
	}

	engine, _ = newEngine(func(context.Context) error { return errors.New("connection refused") })
	status, body := do(t, engine, http.MethodGet, "/healthz", "")
	if status != http.StatusServiceUnavailable {
		t.Errorf("with postgres down: %d %s, want 503", status, body)
	}
}

func TestReadyzStopsAdmittingTrafficBeforeShutdown(t *testing.T) {
	engine, ready := newEngine(func(context.Context) error { return nil })
	if status, _ := do(t, engine, http.MethodGet, "/readyz", ""); status != http.StatusOK {
		t.Fatalf("ready state returned %d", status)
	}

	ready.Set(false)
	status, body := do(t, engine, http.MethodGet, "/readyz", "")
	if status != http.StatusServiceUnavailable {
		t.Errorf("draining state returned %d %s, want 503", status, body)
	}
	var fields map[string]string
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("body %s is not json: %v", body, err)
	}
	if fields["status"] != "shutting_down" {
		t.Errorf("status = %q, want shutting_down", fields["status"])
	}
}
