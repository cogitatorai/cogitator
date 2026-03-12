package drain

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDrainManager(t *testing.T) {
	dm := New()

	t.Run("not draining by default", func(t *testing.T) {
		if dm.IsDraining() {
			t.Fatal("should not be draining initially")
		}
	})

	t.Run("starts draining", func(t *testing.T) {
		dm.Start()
		if !dm.IsDraining() {
			t.Fatal("should be draining after Start()")
		}
	})

	t.Run("start is idempotent", func(t *testing.T) {
		dm.Start()
		if !dm.IsDraining() {
			t.Fatal("should still be draining after second Start()")
		}
	})
}

func TestDrainMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("passes through when not draining", func(t *testing.T) {
		dm := New()
		handler := dm.Middleware()(ok)

		req := httptest.NewRequest("GET", "/api/chat", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("rejects non-internal requests when draining", func(t *testing.T) {
		dm := New()
		dm.Start()
		handler := dm.Middleware()(ok)

		req := httptest.NewRequest("GET", "/api/chat", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
		if rec.Header().Get("Retry-After") != "30" {
			t.Fatalf("expected Retry-After header of 30, got %q", rec.Header().Get("Retry-After"))
		}
	})

	t.Run("allows internal requests when draining", func(t *testing.T) {
		dm := New()
		dm.Start()
		handler := dm.Middleware()(ok)

		req := httptest.NewRequest("GET", "/api/internal/metrics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for internal, got %d", rec.Code)
		}
	})

	t.Run("allows health check when draining", func(t *testing.T) {
		dm := New()
		dm.Start()
		handler := dm.Middleware()(ok)

		req := httptest.NewRequest("GET", "/api/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for health, got %d", rec.Code)
		}
	})
}
