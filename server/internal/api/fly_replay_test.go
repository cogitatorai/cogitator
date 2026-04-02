package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlyReplayMiddleware_HostMatches(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := flyReplayMiddleware("starter.cogitator.cloud", inner)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "starter.cogitator.cloud"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("fly-replay") != "" {
		t.Fatal("fly-replay header should not be set when host matches")
	}
}

func TestFlyReplayMiddleware_HostMismatch(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})
	handler := flyReplayMiddleware("starter.cogitator.cloud", inner)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "pro.cogitator.cloud"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if rec.Header().Get("fly-replay") == "" {
		t.Fatal("fly-replay header should be set on mismatch")
	}
}

func TestFlyReplayMiddleware_HostWithPort(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := flyReplayMiddleware("starter.cogitator.cloud", inner)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "starter.cogitator.cloud:443"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestFlyReplayMiddleware_EmptyHost(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})
	handler := flyReplayMiddleware("starter.cogitator.cloud", inner)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}
