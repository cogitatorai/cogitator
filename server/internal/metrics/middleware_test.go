package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddleware(t *testing.T) {
	ring := NewRing(100)
	handler := Middleware(ring)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	snap := ring.Snapshot()
	if snap.RequestCount != 1 {
		t.Fatalf("expected 1 request recorded, got %d", snap.RequestCount)
	}
	if snap.P95LatencyMs < 1 {
		t.Fatalf("expected latency >= 1ms, got %.2f", snap.P95LatencyMs)
	}
}

func TestMiddlewareRecordsStatusCode(t *testing.T) {
	ring := NewRing(100)
	handler := Middleware(ring)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	snap := ring.Snapshot()
	if snap.RequestCount != 1 {
		t.Fatalf("expected 1 request recorded, got %d", snap.RequestCount)
	}
	if snap.ErrorRate != 1.0 {
		t.Fatalf("expected error rate 1.0, got %.2f", snap.ErrorRate)
	}
}

func TestMiddlewareDefaultStatus(t *testing.T) {
	ring := NewRing(100)
	handler := Middleware(ring)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler writes body without explicit WriteHeader; default is 200.
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	snap := ring.Snapshot()
	if snap.ErrorRate != 0 {
		t.Fatalf("expected error rate 0, got %.2f", snap.ErrorRate)
	}
}
