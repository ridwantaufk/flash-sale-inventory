package inventory

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestRouter(f *fakeRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(newService(f), quietLog())

	engine := gin.New()
	engine.Use(httpx.RequestID())
	v1 := engine.Group("/api/v1/inventory")
	v1.POST("/reserve", h.Reserve)
	v1.POST("/confirm", h.Confirm)
	v1.GET("/stock", h.Stock)
	return engine
}

func post(t *testing.T, engine *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("body is not a json object: %v (%s)", err, raw)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The spec prints these bodies literally, so the key set and the timestamp
// format are part of the contract, not an implementation detail.
func TestReserveBodyMatchesTheSpecExample(t *testing.T) {
	engine := newTestRouter(newFakeRepo())

	w := post(t, engine, "/api/v1/inventory/reserve", `{"user_id":"usr_9981","item_id":"item_4021","quantity":2}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body)
	}

	want := []string{"expires_at", "item_id", "quantity", "reservation_id", "status"}
	if got := jsonKeys(t, w.Body.Bytes()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", got, want)
	}

	var body struct {
		Status    string `json:"status"`
		ItemID    string `json:"item_id"`
		Quantity  int    `json:"quantity"`
		ReserveID string `json:"reservation_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "success" || body.ItemID != "item_4021" || body.Quantity != 2 {
		t.Errorf("body = %+v", body)
	}
	if !strings.HasPrefix(body.ReserveID, "res_") {
		t.Errorf("reservation_id = %q, want the res_ prefix", body.ReserveID)
	}
	parsed, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		t.Fatalf("expires_at %q is not RFC3339: %v", body.ExpiresAt, err)
	}
	if !strings.HasSuffix(body.ExpiresAt, "Z") || parsed.Location() != time.UTC {
		t.Errorf("expires_at = %q, want UTC", body.ExpiresAt)
	}
}

func TestConfirmBodyMatchesTheSpecExample(t *testing.T) {
	f := newFakeRepo()
	engine := newTestRouter(f)

	if _, err := f.Reserve(t.Context(), "item_4021", "usr_9981", 2, time.Minute); err != nil {
		t.Fatal(err)
	}
	w := post(t, engine, "/api/v1/inventory/confirm", `{"reservation_id":"res_usr_9981"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body)
	}

	want := []string{"confirmed_at", "reservation_id", "status"}
	if got := jsonKeys(t, w.Body.Bytes()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func TestStockBodyCarriesNoEnvelopeWrapper(t *testing.T) {
	engine := newTestRouter(newFakeRepo())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/stock?item_id=item_4021", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body)
	}
	want := []string{"available_stock", "item_id", "reserved_stock", "total_stock"}
	if got := jsonKeys(t, w.Body.Bytes()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func TestEachDomainErrorHasExactlyOneHttpStatus(t *testing.T) {
	cases := []struct {
		name       string
		repoErr    error
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"unknown item", ErrItemNotFound, "POST", "/reserve", `{"user_id":"u","item_id":"ghost","quantity":1}`, http.StatusNotFound, "ITEM_NOT_FOUND"},
		{"not enough stock", ErrInsufficientStock, "POST", "/reserve", `{"user_id":"u","item_id":"item_4021","quantity":99}`, http.StatusConflict, "INSUFFICIENT_STOCK"},
		{"unknown reservation", ErrReservationNotFound, "POST", "/confirm", `{"reservation_id":"res_ghost"}`, http.StatusNotFound, "RESERVATION_NOT_FOUND"},
		{"already confirmed", ErrAlreadyConfirmed, "POST", "/confirm", `{"reservation_id":"res_u"}`, http.StatusConflict, "ALREADY_CONFIRMED"},
		{"expired", ErrReservationExpired, "POST", "/confirm", `{"reservation_id":"res_u"}`, http.StatusConflict, "RESERVATION_EXPIRED"},
		{"counter drift", ErrStockInvariant, "POST", "/confirm", `{"reservation_id":"res_u"}`, http.StatusInternalServerError, "STOCK_INVARIANT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRepo()
			f.err = tc.repoErr
			w := post(t, newTestRouter(f), "/api/v1/inventory"+tc.path, tc.body)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", w.Code, tc.wantStatus, w.Body)
			}
			var body struct {
				Status string `json:"status"`
				Code   string `json:"code"`
				Msg    string `json:"message"`
				ReqID  string `json:"request_id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("error body is not json: %v (%s)", err, w.Body)
			}
			if body.Status != "error" || body.Code != tc.wantCode {
				t.Errorf("body = %+v, want status error and code %s", body, tc.wantCode)
			}
			if body.Msg == "" || body.ReqID == "" {
				t.Errorf("body = %+v, want a message and a request id", body)
			}
		})
	}
}

func TestInvalidQuantityPointsAtTheOffendingField(t *testing.T) {
	w := post(t, newTestRouter(newFakeRepo()), "/api/v1/inventory/reserve",
		`{"user_id":"usr_9981","item_id":"item_4021","quantity":0}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", w.Code, w.Body)
	}
	var body struct {
		Code    string `json:"code"`
		Details struct {
			Field  string `json:"field"`
			Reason string `json:"reason"`
		} `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "INVALID_INPUT" || body.Details.Field != "quantity" || body.Details.Reason == "" {
		t.Errorf("body = %+v", body)
	}
}

func TestMalformedJsonNeverReachesTheRepository(t *testing.T) {
	f := newFakeRepo()
	w := post(t, newTestRouter(f), "/api/v1/inventory/reserve", `{"user_id":`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (%s)", w.Code, w.Body)
	}
	if f.reserveCalls != 0 {
		t.Error("repository was called with an unparseable body")
	}
}

func TestStockRejectsBlankItemIDBeforeHittingPostgres(t *testing.T) {
	f := newFakeRepo()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/inventory/stock", nil)
	w := httptest.NewRecorder()
	newTestRouter(f).ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 (%s)", w.Code, w.Body)
	}
	if f.stockCalls != 0 {
		t.Error("repository was called for a missing item_id")
	}
}
