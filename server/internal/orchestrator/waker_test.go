package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaker_WakesDueTenants(t *testing.T) {
	db := newTestDB(t)

	// Insert a tenant with a wake_schedule entry in the past.
	_, err := db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, tier, jwt_secret, status)
		 VALUES ('t1', 'acct_1', 'acme', 'free', 'secret', 'active')`,
	)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO wake_schedule (tenant_id, wake_at) VALUES ('t1', datetime('now', '-1 minute'))`,
	)
	if err != nil {
		t.Fatalf("insert wake_schedule: %v", err)
	}

	// Mock health endpoint.
	var woken atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		woken.Add(1)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer mock.Close()

	w := NewWaker(db)
	w.baseURLFmt = mock.URL + "/%s"

	// Run one tick manually instead of relying on the ticker.
	w.tick()

	if woken.Load() != 1 {
		t.Fatalf("expected 1 wake call, got %d", woken.Load())
	}

	// The wake_schedule row should be deleted.
	var count int
	db.db.QueryRow(`SELECT COUNT(*) FROM wake_schedule WHERE tenant_id = 't1'`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected wake_schedule row to be deleted, got count %d", count)
	}
}

func TestWaker_LeavesRowOnFailure(t *testing.T) {
	db := newTestDB(t)

	_, err := db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, tier, jwt_secret, status)
		 VALUES ('t1', 'acct_1', 'fail-tenant', 'free', 'secret', 'active')`,
	)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO wake_schedule (tenant_id, wake_at) VALUES ('t1', datetime('now', '-1 minute'))`,
	)
	if err != nil {
		t.Fatalf("insert wake_schedule: %v", err)
	}

	// Mock that always fails.
	var attempts atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mock.Close()

	w := NewWaker(db)
	w.baseURLFmt = mock.URL + "/%s"
	w.retryDelay = 10 * time.Millisecond // Speed up retries for testing.

	w.tick()

	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}

	// Row should still exist.
	var count int
	db.db.QueryRow(`SELECT COUNT(*) FROM wake_schedule WHERE tenant_id = 't1'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected wake_schedule row to remain, got count %d", count)
	}
}

func TestWaker_IgnoresFutureSchedules(t *testing.T) {
	db := newTestDB(t)

	_, err := db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, tier, jwt_secret, status)
		 VALUES ('t1', 'acct_1', 'future', 'free', 'secret', 'active')`,
	)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO wake_schedule (tenant_id, wake_at) VALUES ('t1', datetime('now', '+10 minutes'))`,
	)
	if err != nil {
		t.Fatalf("insert wake_schedule: %v", err)
	}

	var woken atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		woken.Add(1)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer mock.Close()

	w := NewWaker(db)
	w.baseURLFmt = mock.URL + "/%s"

	w.tick()

	if woken.Load() != 0 {
		t.Fatalf("expected 0 wake calls for future schedule, got %d", woken.Load())
	}

	// Row should still exist.
	var count int
	db.db.QueryRow(`SELECT COUNT(*) FROM wake_schedule WHERE tenant_id = 't1'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected wake_schedule row to remain, got count %d", count)
	}
}

func TestWaker_StartStop(t *testing.T) {
	db := newTestDB(t)

	w := NewWaker(db)
	w.Start()

	// Verify it can be stopped cleanly without blocking.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good.
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return within 3 seconds")
	}
}
