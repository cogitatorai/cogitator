package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInternalAuthMiddleware(t *testing.T) {
	secret := "test-secret-123"
	handler := internalAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("rejects missing header", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/internal/drain", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("rejects wrong secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/internal/drain", nil)
		req.Header.Set("X-Internal-Secret", "wrong")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("accepts correct secret", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/internal/drain", nil)
		req.Header.Set("X-Internal-Secret", secret)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}
