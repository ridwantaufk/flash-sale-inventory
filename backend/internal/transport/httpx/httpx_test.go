package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newContext(inboundID string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/inventory/stock", nil)
	if inboundID != "" {
		c.Request.Header.Set("X-Request-ID", inboundID)
	}
	RequestID()(c)
	return c, w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a json object: %v (%s)", err, w.Body)
	}
	return body
}

func TestErrorEnvelopeCarriesCodeAndRequestID(t *testing.T) {
	c, w := newContext("req_supplied_by_client")

	Error(c, http.StatusConflict, "INSUFFICIENT_STOCK", "not enough units", nil)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	body := decode(t, w)
	if body["status"] != "error" || body["code"] != "INSUFFICIENT_STOCK" {
		t.Errorf("body = %v", body)
	}
	if body["request_id"] != "req_supplied_by_client" {
		t.Errorf("request_id = %v, want the caller's value echoed back", body["request_id"])
	}
	if _, present := body["details"]; present {
		t.Error("details should be absent when there is nothing to point at")
	}
	if w.Header().Get("X-Request-ID") != "req_supplied_by_client" {
		t.Error("the response header must carry the same id as the body")
	}
}

func TestErrorDetailsNameTheField(t *testing.T) {
	c, w := newContext("")
	Error(c, http.StatusUnprocessableEntity, "INVALID_INPUT", "must be at least 1",
		ErrorDetails("quantity", "must be at least 1"))

	details, ok := decode(t, w)["details"].(map[string]any)
	if !ok {
		t.Fatal("details missing from the envelope")
	}
	if details["field"] != "quantity" || details["reason"] != "must be at least 1" {
		t.Errorf("details = %v", details)
	}
}

func TestInternalDoesNotLeakTheUnderlyingError(t *testing.T) {
	c, w := newContext("")
	Internal(c, errors.New(`pq: password authentication failed for user "flashsale"`))

	body := decode(t, w)
	if body["code"] != "INTERNAL" || body["message"] != "internal server error" {
		t.Errorf("body = %v", body)
	}
	if strings.Contains(w.Body.String(), "flashsale") {
		t.Errorf("the envelope leaks connection details: %s", w.Body)
	}
}

func TestGeneratedRequestIDsDoNotCollide(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		c, _ := newContext("")
		RequestID()(c)
		id := ID(c)
		if id == "" {
			t.Fatal("no request id was generated")
		}
		if seen[id] {
			t.Fatalf("duplicate request id %q on iteration %d", id, i)
		}
		seen[id] = true
	}
}

func TestReadinessFlipsBeforeTheListenerCloses(t *testing.T) {
	ready := NewReadiness()
	engine := gin.New()
	engine.GET("/readyz", ready.Handler())

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 while ready", w.Code)
	}

	ready.Set(false)
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 while shutting down", w.Code)
	}
}
