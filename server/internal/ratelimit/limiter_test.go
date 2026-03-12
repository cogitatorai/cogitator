package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(10) // 10 per minute for fast testing
	defer l.Stop()

	t.Run("allows requests within limit", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			if !l.Allow("1.2.3.4") {
				t.Fatalf("request %d should be allowed", i)
			}
		}
	})

	t.Run("rejects after burst exhausted", func(t *testing.T) {
		if l.Allow("1.2.3.4") {
			t.Fatal("11th request should be rejected")
		}
	})

	t.Run("different IPs have separate buckets", func(t *testing.T) {
		if !l.Allow("5.6.7.8") {
			t.Fatal("different IP should be allowed")
		}
	})

	t.Run("tokens refill over time", func(t *testing.T) {
		l2 := New(60) // 1 per second
		defer l2.Stop()
		for i := 0; i < 60; i++ {
			l2.Allow("10.0.0.1")
		}
		if l2.Allow("10.0.0.1") {
			t.Fatal("should be rate limited")
		}
		time.Sleep(1100 * time.Millisecond)
		if !l2.Allow("10.0.0.1") {
			t.Fatal("should be allowed after refill")
		}
	})

	t.Run("middleware returns 429", func(t *testing.T) {
		l3 := New(1)
		defer l3.Stop()
		handler := l3.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))

		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "9.8.7.6:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 429 {
			t.Fatalf("expected 429, got %d", rec.Code)
		}
		if rec.Header().Get("Retry-After") != "1" {
			t.Fatal("expected Retry-After header")
		}
	})
}

func TestLimiterCleanup(t *testing.T) {
	l := New(10)
	defer l.Stop()

	// Create some entries.
	l.Allow("10.0.0.1")
	l.Allow("10.0.0.2")

	// Verify entries exist.
	count := 0
	l.buckets.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 2 {
		t.Fatalf("expected 2 buckets, got %d", count)
	}

	// Backdate one entry so it appears stale.
	val, ok := l.buckets.Load("10.0.0.1")
	if !ok {
		t.Fatal("expected bucket for 10.0.0.1")
	}
	b := val.(*bucket)
	b.mu.Lock()
	b.lastCheck = time.Now().Add(-10 * time.Minute)
	b.mu.Unlock()

	// Run cleanup.
	l.cleanup()

	// Stale entry should be gone, fresh entry should remain.
	_, staleExists := l.buckets.Load("10.0.0.1")
	_, freshExists := l.buckets.Load("10.0.0.2")
	if staleExists {
		t.Error("expected stale bucket 10.0.0.1 to be cleaned up")
	}
	if !freshExists {
		t.Error("expected fresh bucket 10.0.0.2 to remain")
	}
}
