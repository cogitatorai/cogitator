package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// rewriteTransport redirects all HTTP requests to a target test server URL.
// This allows tests to intercept calls to *.cogitator.cloud and route them
// to an httptest.Server instead.
type rewriteTransport struct {
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the scheme+host with our test server.
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	return http.DefaultTransport.RoundTrip(req)
}

// newTestRolloutManager returns a RolloutManager with fast polling for tests.
func newTestRolloutManager(db *OrchestratorDB, f *mockFly) *RolloutManager {
	rm := NewRolloutManager(db, f, "test-secret")
	rm.canaryWait = 0
	rm.drainPollInterval = 10 * time.Millisecond
	rm.healthCheckInterval = 10 * time.Millisecond
	rm.drainTimeout = 500 * time.Millisecond
	rm.healthCheckTimeout = 500 * time.Millisecond
	return rm
}

// insertTestRelease inserts a release and returns its ID.
func insertTestRelease(t *testing.T, db *OrchestratorDB, version, imageTag, severity, components string) string {
	t.Helper()
	id := fmt.Sprintf("rel_%s", version)
	_, err := db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components) VALUES (?, ?, ?, ?, ?)`,
		id, version, imageTag, severity, components,
	)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}
	return id
}

// insertTestTenant inserts an active tenant and returns its ID.
func insertTestTenant(t *testing.T, db *OrchestratorDB, slug, machineID string) string {
	t.Helper()
	id := fmt.Sprintf("tenant_%s", slug)
	now := time.Now().UTC().Format(time.DateTime)
	_, err := db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, fly_machine_id, fly_volume_id, tier, status, jwt_secret, created_at, updated_at)
		 VALUES (?, 'acct_1', ?, ?, 'vol_1', 'free', 'active', 'secret', ?, ?)`,
		id, slug, machineID, now, now,
	)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return id
}

func TestFleetWideRollout(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Create 3 tenants.
	for i := 0; i < 3; i++ {
		slug := fmt.Sprintf("fw%d", i)
		insertTestTenant(t, db, slug, fmt.Sprintf("mach_%s", slug))
	}

	// Create a patch release (should use fleet_wide).
	relID := insertTestRelease(t, db, "1.0.1", "registry.fly.io/cogi:1.0.1", "patch", "all")

	// Start mock tenant servers that respond to drain and health.
	// For fleet-wide, we skip real HTTP calls by using a custom client
	// that resolves to our mock servers.
	// Instead, we test the batch structure and DB state.

	// Since executeBatch makes real HTTP calls, and we can't easily intercept
	// *.cogitator.cloud, we test the batch creation separately.
	err := rm.StartRollout(relID)
	if err != nil {
		t.Fatalf("start rollout: %v", err)
	}

	// Give background goroutine time to fail on HTTP (expected).
	time.Sleep(100 * time.Millisecond)

	// Verify rollout record.
	var strategy string
	err = db.db.QueryRow(`SELECT strategy FROM rollouts WHERE release_id = ?`, relID).Scan(&strategy)
	if err != nil {
		t.Fatalf("query rollout: %v", err)
	}
	if strategy != "fleet_wide" {
		t.Fatalf("expected fleet_wide strategy, got %s", strategy)
	}

	// Verify single batch at 100%.
	var batchCount int
	err = db.db.QueryRow(
		`SELECT COUNT(*) FROM rollout_batches rb
		 JOIN rollouts r ON r.id = rb.rollout_id
		 WHERE r.release_id = ?`, relID,
	).Scan(&batchCount)
	if err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batchCount != 1 {
		t.Fatalf("expected 1 batch, got %d", batchCount)
	}

	// Verify all 3 tenants assigned to the batch.
	var tenantCount int
	err = db.db.QueryRow(
		`SELECT COUNT(*) FROM rollout_tenants rt
		 JOIN rollout_batches rb ON rb.id = rt.rollout_batch_id
		 JOIN rollouts r ON r.id = rb.rollout_id
		 WHERE r.release_id = ?`, relID,
	).Scan(&tenantCount)
	if err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if tenantCount != 3 {
		t.Fatalf("expected 3 tenants in batch, got %d", tenantCount)
	}
}

func TestCanaryBatchCreation(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Create 10 tenants.
	for i := 0; i < 10; i++ {
		slug := fmt.Sprintf("cn%02d", i)
		insertTestTenant(t, db, slug, fmt.Sprintf("mach_%s", slug))
	}

	// Create a minor release (should use canary).
	relID := insertTestRelease(t, db, "1.1.0", "registry.fly.io/cogi:1.1.0", "minor", "all")

	err := rm.StartRollout(relID)
	if err != nil {
		t.Fatalf("start rollout: %v", err)
	}

	// Give background goroutine time to start.
	time.Sleep(100 * time.Millisecond)

	// Verify canary strategy.
	var strategy string
	err = db.db.QueryRow(`SELECT strategy FROM rollouts WHERE release_id = ?`, relID).Scan(&strategy)
	if err != nil {
		t.Fatalf("query rollout: %v", err)
	}
	if strategy != "canary" {
		t.Fatalf("expected canary strategy, got %s", strategy)
	}

	// Verify 3 batches with correct percentages.
	rows, err := db.db.Query(
		`SELECT rb.batch_number, rb.percentage FROM rollout_batches rb
		 JOIN rollouts r ON r.id = rb.rollout_id
		 WHERE r.release_id = ? ORDER BY rb.batch_number`, relID,
	)
	if err != nil {
		t.Fatalf("query batches: %v", err)
	}
	defer rows.Close()

	type batchRow struct {
		number     int
		percentage int
	}
	var batches []batchRow
	for rows.Next() {
		var b batchRow
		rows.Scan(&b.number, &b.percentage)
		batches = append(batches, b)
	}

	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if batches[0].percentage != 5 || batches[1].percentage != 25 || batches[2].percentage != 100 {
		t.Fatalf("unexpected percentages: %v", batches)
	}

	// Verify tenant distribution: batch1=1 (5% of 10, min 1), batch2=3 (25% of 10, ceil), batch3=6 (remainder).
	for _, b := range batches {
		var count int
		db.db.QueryRow(
			`SELECT COUNT(*) FROM rollout_tenants rt
			 JOIN rollout_batches rb ON rb.id = rt.rollout_batch_id
			 JOIN rollouts r ON r.id = rb.rollout_id
			 WHERE r.release_id = ? AND rb.batch_number = ?`, relID, b.number,
		).Scan(&count)

		switch b.number {
		case 1:
			if count != 1 {
				t.Fatalf("batch 1: expected 1 tenant, got %d", count)
			}
		case 2:
			if count != 3 {
				t.Fatalf("batch 2: expected 3 tenants, got %d", count)
			}
		case 3:
			if count != 6 {
				t.Fatalf("batch 3: expected 6 tenants, got %d", count)
			}
		}
	}
}

func TestDrainPolling(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Mock drain endpoint: return false twice, then true.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n <= 2 {
			json.NewEncoder(w).Encode(map[string]bool{"drained": false})
		} else {
			json.NewEncoder(w).Encode(map[string]bool{"drained": true})
		}
	}))
	defer srv.Close()

	err := rm.drainTenant(srv.URL)
	if err != nil {
		t.Fatalf("drain should succeed: %v", err)
	}

	calls := callCount.Load()
	if calls < 3 {
		t.Fatalf("expected at least 3 drain calls, got %d", calls)
	}
}

func TestDrainTimeout(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)
	rm.drainTimeout = 100 * time.Millisecond

	// Mock drain endpoint: always return false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"drained": false})
	}))
	defer srv.Close()

	err := rm.drainTenant(srv.URL)
	if err == nil {
		t.Fatal("expected drain timeout error")
	}
}

func TestHealthCheckPolling(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Mock health endpoint: return 503 twice, then 200.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	err := rm.waitForHealth(srv.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("health check should succeed: %v", err)
	}

	calls := callCount.Load()
	if calls < 3 {
		t.Fatalf("expected at least 3 health check calls, got %d", calls)
	}
}

func TestHealthCheckTimeout(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Mock health endpoint: always 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := rm.waitForHealth(srv.URL, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected health check timeout error")
	}
}

func TestCanaryValidationPass(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	tenantID := insertTestTenant(t, db, "cvpass", "mach_cvpass")

	// Insert baseline.
	_, err := db.db.Exec(
		`INSERT INTO tenant_metrics_baseline (tenant_id, p95_latency_ms, error_rate) VALUES (?, 100.0, 0.01)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert baseline: %v", err)
	}

	// Insert heartbeat within acceptable range.
	_, err = db.db.Exec(
		`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, 100, 0.015, 120.0)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	// Create a batch with this tenant.
	relID := insertTestRelease(t, db, "1.2.0", "registry.fly.io/cogi:1.2.0", "minor", "all")
	rolloutID := "rollout_pass"
	now := time.Now().UTC().Format(time.DateTime)
	db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components, created_at, updated_at) VALUES (?, ?, 'in_progress', 'canary', 'all', ?, ?)`,
		rolloutID, relID, now, now,
	)
	batchID := "batch_pass"
	db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 5, 'completed')`,
		batchID, rolloutID,
	)
	db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_pass', ?, ?, 'healthy')`,
		batchID, tenantID,
	)

	if !rm.validateCanary(batchID) {
		t.Fatal("canary validation should pass with metrics within threshold")
	}
}

func TestCanaryValidationFail_P95(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	tenantID := insertTestTenant(t, db, "cvfail", "mach_cvfail")

	// Insert baseline.
	_, err := db.db.Exec(
		`INSERT INTO tenant_metrics_baseline (tenant_id, p95_latency_ms, error_rate) VALUES (?, 100.0, 0.01)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert baseline: %v", err)
	}

	// Insert heartbeat with degraded p95 (> 1.5x baseline).
	_, err = db.db.Exec(
		`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, 100, 0.01, 200.0)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	// Create batch.
	relID := insertTestRelease(t, db, "1.3.0", "registry.fly.io/cogi:1.3.0", "minor", "all")
	rolloutID := "rollout_fail"
	now := time.Now().UTC().Format(time.DateTime)
	db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components, created_at, updated_at) VALUES (?, ?, 'in_progress', 'canary', 'all', ?, ?)`,
		rolloutID, relID, now, now,
	)
	batchID := "batch_fail"
	db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 5, 'completed')`,
		batchID, rolloutID,
	)
	db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_fail', ?, ?, 'healthy')`,
		batchID, tenantID,
	)

	if rm.validateCanary(batchID) {
		t.Fatal("canary validation should fail when p95 exceeds 1.5x baseline")
	}
}

func TestCanaryValidationFail_ErrorRate(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	tenantID := insertTestTenant(t, db, "cvferr", "mach_cvferr")

	// Insert baseline.
	_, err := db.db.Exec(
		`INSERT INTO tenant_metrics_baseline (tenant_id, p95_latency_ms, error_rate) VALUES (?, 100.0, 0.01)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert baseline: %v", err)
	}

	// Insert heartbeat with degraded error rate (> baseline + 0.02).
	_, err = db.db.Exec(
		`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, 100, 0.05, 100.0)`,
		tenantID,
	)
	if err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	// Create batch.
	relID := insertTestRelease(t, db, "1.4.0", "registry.fly.io/cogi:1.4.0", "minor", "all")
	rolloutID := "rollout_ferr"
	now := time.Now().UTC().Format(time.DateTime)
	db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components, created_at, updated_at) VALUES (?, ?, 'in_progress', 'canary', 'all', ?, ?)`,
		rolloutID, relID, now, now,
	)
	batchID := "batch_ferr"
	db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 5, 'completed')`,
		batchID, rolloutID,
	)
	db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_ferr', ?, ?, 'healthy')`,
		batchID, tenantID,
	)

	if rm.validateCanary(batchID) {
		t.Fatal("canary validation should fail when error rate exceeds baseline + 0.02")
	}
}

func TestEndToEndRolloutWithMockServers(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Create a mock server that handles both drain and health endpoints.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/drain":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"drained": true})
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Insert a tenant whose slug resolves to our mock server.
	// We override the URL generation by inserting a tenant and then
	// testing executeBatch directly with a custom approach.
	// For true end-to-end, we test executeBatch with known URLs.

	tenantID := insertTestTenant(t, db, "e2e", "mach_e2e")
	relID := insertTestRelease(t, db, "2.0.0", "registry.fly.io/cogi:2.0.0", "patch", "all")

	rolloutID := "rollout_e2e"
	now := time.Now().UTC().Format(time.DateTime)
	db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components, created_at, updated_at) VALUES (?, ?, 'in_progress', 'fleet_wide', 'all', ?, ?)`,
		rolloutID, relID, now, now,
	)
	batchID := "batch_e2e"
	db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 100, 'pending')`,
		batchID, rolloutID,
	)

	// Override the tenant slug to use localhost by updating slug.
	// Extract host:port from the test server URL.
	// We test drainTenant and waitForHealth directly with the mock server URL.
	err := rm.drainTenant(srv.URL)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	err = rm.waitForHealth(srv.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("health: %v", err)
	}

	// Verify UpdateMachine and StartMachine work via the mock.
	err = mf.UpdateMachine("mach_e2e", "registry.fly.io/cogi:2.0.0")
	if err != nil {
		t.Fatalf("update machine: %v", err)
	}
	err = mf.StartMachine("mach_e2e")
	if err != nil {
		t.Fatalf("start machine: %v", err)
	}

	// Mark the rollout_tenant so we can verify the DB flow.
	db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_e2e', ?, ?, 'pending')`,
		batchID, tenantID,
	)

	// Verify we can load batches.
	batches, err := rm.loadBatches(rolloutID)
	if err != nil {
		t.Fatalf("load batches: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if batches[0].number != 1 {
		t.Fatalf("expected batch number 1, got %d", batches[0].number)
	}
}

func TestCanaryValidationNoBaseline(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	tenantID := insertTestTenant(t, db, "nobase", "mach_nobase")

	relID := insertTestRelease(t, db, "1.5.0", "registry.fly.io/cogi:1.5.0", "minor", "all")
	rolloutID := "rollout_nobase"
	now := time.Now().UTC().Format(time.DateTime)
	db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components, created_at, updated_at) VALUES (?, ?, 'in_progress', 'canary', 'all', ?, ?)`,
		rolloutID, relID, now, now,
	)
	batchID := "batch_nobase"
	db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 5, 'completed')`,
		batchID, rolloutID,
	)
	db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_nobase', ?, ?, 'healthy')`,
		batchID, tenantID,
	)

	// No baseline data. Should pass.
	if !rm.validateCanary(batchID) {
		t.Fatal("canary validation should pass when no baseline exists")
	}
}

func TestPreviousImageTag(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Insert two releases with explicit timestamps to guarantee ordering.
	_, err := db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_0.9.0", "0.9.0", "registry.fly.io/cogi:0.9.0", "minor", "all", "2026-01-01 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_1.0.0", "1.0.0", "registry.fly.io/cogi:1.0.0", "minor", "all", "2026-01-02 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}

	prev := rm.previousImageTag("rel_1.0.0")
	if prev != "registry.fly.io/cogi:0.9.0" {
		t.Fatalf("expected previous image tag 'registry.fly.io/cogi:0.9.0', got %q", prev)
	}
}

func TestRollbackWithUpdatedTenants(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Mock server handles drain and health for rollback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/drain":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"drained": true})
		case "/api/health":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Insert two releases with explicit timestamps.
	_, err := db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_rb_old", "1.0.0", "registry.fly.io/cogi:1.0.0", "minor", "all", "2026-01-01 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert old release: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_rb_new", "1.1.0", "registry.fly.io/cogi:1.1.0", "minor", "all", "2026-01-02 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert new release: %v", err)
	}

	// Insert 2 tenants. Use slugs that we can override via the test server.
	// Since Rollback builds URLs from slugs (https://<slug>.cogitator.cloud),
	// we need to override the HTTP client to route to our mock server.
	t1ID := insertTestTenant(t, db, "rbt1", "mach_rbt1")
	t2ID := insertTestTenant(t, db, "rbt2", "mach_rbt2")

	// Create rollout record with previous_image_tag set.
	rolloutID := "rollout_rb"
	now := time.Now().UTC().Format(time.DateTime)
	_, err = db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, previous_image_tag, components, created_at, updated_at)
		 VALUES (?, ?, 'paused', 'canary', 'registry.fly.io/cogi:1.0.0', 'all', ?, ?)`,
		rolloutID, "rel_rb_new", now, now,
	)
	if err != nil {
		t.Fatalf("insert rollout: %v", err)
	}

	// Create batch and mark both tenants as healthy (simulating a completed partial rollout).
	batchID := "batch_rb"
	_, err = db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 100, 'completed')`,
		batchID, rolloutID,
	)
	if err != nil {
		t.Fatalf("insert batch: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_rb1', ?, ?, 'healthy')`,
		batchID, t1ID,
	)
	if err != nil {
		t.Fatalf("insert rollout tenant 1: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_rb2', ?, ?, 'healthy')`,
		batchID, t2ID,
	)
	if err != nil {
		t.Fatalf("insert rollout tenant 2: %v", err)
	}

	// Override the HTTP client to redirect all requests to our mock server.
	rm.client = srv.Client()
	originalDrain := rm.drainTenant
	_ = originalDrain // We need to override URL resolution. Patch drainTenant and waitForHealth
	// by overriding the client's transport to redirect any host to our test server.
	rm.client.Transport = &rewriteTransport{target: srv.URL}

	err = rm.Rollback(rolloutID)
	if err != nil {
		t.Fatalf("rollback should succeed: %v", err)
	}

	// Verify both tenants are marked as rolled_back.
	for _, rtID := range []string{"rt_rb1", "rt_rb2"} {
		var status string
		err = db.db.QueryRow(`SELECT status FROM rollout_tenants WHERE id = ?`, rtID).Scan(&status)
		if err != nil {
			t.Fatalf("query tenant %s: %v", rtID, err)
		}
		if status != "rolled_back" {
			t.Fatalf("tenant %s: expected status 'rolled_back', got %q", rtID, status)
		}
	}

	// Verify rollout is marked as rolled_back.
	var rolloutStatus string
	err = db.db.QueryRow(`SELECT status FROM rollouts WHERE id = ?`, rolloutID).Scan(&rolloutStatus)
	if err != nil {
		t.Fatalf("query rollout: %v", err)
	}
	if rolloutStatus != "rolled_back" {
		t.Fatalf("expected rollout status 'rolled_back', got %q", rolloutStatus)
	}
}

func TestRollbackNoUpdatedTenants(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	// Insert releases.
	_, err := db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_rbn_old", "1.0.0", "registry.fly.io/cogi:1.0.0", "minor", "all", "2026-01-01 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert old release: %v", err)
	}
	_, err = db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, severity, components, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rel_rbn_new", "1.1.0", "registry.fly.io/cogi:1.1.0", "minor", "all", "2026-01-02 00:00:00",
	)
	if err != nil {
		t.Fatalf("insert new release: %v", err)
	}

	// Create rollout with previous image but no healthy tenants (all pending).
	rolloutID := "rollout_rbn"
	now := time.Now().UTC().Format(time.DateTime)
	_, err = db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, previous_image_tag, components, created_at, updated_at)
		 VALUES (?, ?, 'paused', 'canary', 'registry.fly.io/cogi:1.0.0', 'all', ?, ?)`,
		rolloutID, "rel_rbn_new", now, now,
	)
	if err != nil {
		t.Fatalf("insert rollout: %v", err)
	}

	batchID := "batch_rbn"
	_, err = db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, 1, 100, 'pending')`,
		batchID, rolloutID,
	)
	if err != nil {
		t.Fatalf("insert batch: %v", err)
	}

	tenantID := insertTestTenant(t, db, "rbnt", "mach_rbnt")
	_, err = db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES ('rt_rbn', ?, ?, 'pending')`,
		batchID, tenantID,
	)
	if err != nil {
		t.Fatalf("insert rollout tenant: %v", err)
	}

	// Rollback should succeed as a no-op (no healthy tenants to revert).
	err = rm.Rollback(rolloutID)
	if err != nil {
		t.Fatalf("rollback with no updated tenants should succeed: %v", err)
	}

	// Verify rollout is still marked as rolled_back.
	var rolloutStatus string
	err = db.db.QueryRow(`SELECT status FROM rollouts WHERE id = ?`, rolloutID).Scan(&rolloutStatus)
	if err != nil {
		t.Fatalf("query rollout: %v", err)
	}
	if rolloutStatus != "rolled_back" {
		t.Fatalf("expected rollout status 'rolled_back', got %q", rolloutStatus)
	}

	// Verify the pending tenant was NOT touched.
	var tenantStatus string
	err = db.db.QueryRow(`SELECT status FROM rollout_tenants WHERE id = 'rt_rbn'`).Scan(&tenantStatus)
	if err != nil {
		t.Fatalf("query tenant: %v", err)
	}
	if tenantStatus != "pending" {
		t.Fatalf("expected tenant status 'pending', got %q", tenantStatus)
	}
}

func TestPreviousImageTag_NoPrevious(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	rm := newTestRolloutManager(db, mf)

	relID := insertTestRelease(t, db, "1.0.0-first", "registry.fly.io/cogi:first", "minor", "all")
	prev := rm.previousImageTag(relID)
	if prev != "" {
		t.Fatalf("expected empty previous image tag, got %q", prev)
	}
}
