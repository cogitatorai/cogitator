package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
)

// operatorReq builds an http.Request with the operator context injected directly,
// bypassing JWT validation, and sets the path value for {id} if provided.
func operatorReq(method, path, id string, accountID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	ctx := context.WithValue(req.Context(), ctxAccountID, accountID)
	ctx = context.WithValue(ctx, ctxIsOperator, true)
	req = req.WithContext(ctx)
	if id != "" {
		req.SetPathValue("id", id)
	}
	return req
}

// authedReq builds a request with an operator JWT Bearer token.
func authedReq(t *testing.T, s *Server, method, path, id string, token string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if id != "" {
		req.SetPathValue("id", id)
	}
	return req
}

func TestHandleListTenants(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)

	tenantID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, tier, status, jwt_secret) VALUES (?, ?, ?, ?, ?, ?)`,
		tenantID, ownerID, "test-slug", "free", "active", "secret",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Seed a heartbeat for this tenant.
	_, err = s.db.db.Exec(
		`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, ?, ?, ?)`,
		tenantID, 100, 0.01, 250.0,
	)
	if err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}

	req := authedReq(t, s, http.MethodGet, "/api/tenants", "", opToken)
	rec := httptest.NewRecorder()
	s.handleListTenants(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	tenants, ok := resp["tenants"].([]any)
	if !ok {
		t.Fatalf("expected tenants array, got %T", resp["tenants"])
	}
	if len(tenants) != 1 {
		t.Fatalf("expected 1 tenant, got %d", len(tenants))
	}

	total, ok := resp["total"].(float64)
	if !ok || int(total) != 1 {
		t.Fatalf("expected total=1, got %v", resp["total"])
	}

	tenant := tenants[0].(map[string]any)
	if tenant["id"] != tenantID {
		t.Errorf("expected tenant id %s, got %v", tenantID, tenant["id"])
	}
	if tenant["slug"] != "test-slug" {
		t.Errorf("expected slug test-slug, got %v", tenant["slug"])
	}
	if tenant["last_heartbeat_at"] == nil {
		t.Error("expected last_heartbeat_at to be set")
	}
}

func TestHandleFleetStats(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)

	statuses := []string{"active", "active", "sleeping", "error"}
	for i, st := range statuses {
		id := ulid.Make().String()
		_, err := s.db.db.Exec(
			`INSERT INTO tenants (id, account_id, slug, status, jwt_secret) VALUES (?, ?, ?, ?, ?)`,
			id, ownerID, "slug-"+string(rune('a'+i)), st, "secret",
		)
		if err != nil {
			t.Fatalf("seed tenant %d: %v", i, err)
		}
	}

	req := authedReq(t, s, http.MethodGet, "/api/fleet/stats", "", opToken)
	rec := httptest.NewRecorder()
	s.handleFleetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	assertFloat := func(key string, want float64) {
		t.Helper()
		got, ok := resp[key].(float64)
		if !ok {
			t.Fatalf("expected %s to be a number, got %T (%v)", key, resp[key], resp[key])
		}
		if got != want {
			t.Errorf("expected %s=%v, got %v", key, want, got)
		}
	}

	assertFloat("total", 4)
	assertFloat("active", 2)
	assertFloat("sleeping", 1)
	assertFloat("error", 1)
	assertFloat("provisioning", 0)

	// No active rollout seeded, so active_rollout should be nil.
	if resp["active_rollout"] != nil {
		t.Errorf("expected active_rollout=nil, got %v", resp["active_rollout"])
	}
}

func TestHandleFleetStatsEmptyDB(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	req := authedReq(t, s, http.MethodGet, "/api/fleet/stats", "", opToken)
	rec := httptest.NewRecorder()
	s.handleFleetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on empty DB, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"total", "active", "sleeping", "error", "provisioning"} {
		got, ok := resp[key].(float64)
		if !ok {
			t.Fatalf("expected %s to be a number, got %T (%v)", key, resp[key], resp[key])
		}
		if got != 0 {
			t.Errorf("expected %s=0 on empty DB, got %v", key, got)
		}
	}
}

func TestHandleTenantDetail(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)

	tenantID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, tier, status, fly_machine_id, fly_volume_id, jwt_secret) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, ownerID, "detail-slug", "pro", "active", "mach-1", "vol-1", "secret",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Seed wake schedule.
	_, err = s.db.db.Exec(
		`INSERT INTO wake_schedule (tenant_id, wake_at) VALUES (?, ?)`,
		tenantID, "2026-04-01T08:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed wake_schedule: %v", err)
	}

	// Seed subscription.
	subID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO subscriptions (id, tenant_id, stripe_customer_id, tier, status, current_period_end) VALUES (?, ?, ?, ?, ?, ?)`,
		subID, tenantID, "cus_abc", "pro", "active", "2026-04-30T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	req := authedReq(t, s, http.MethodGet, "/api/tenants/"+tenantID, tenantID, opToken)
	rec := httptest.NewRecorder()
	s.handleTenantDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["id"] != tenantID {
		t.Errorf("expected id=%s, got %v", tenantID, resp["id"])
	}
	if resp["slug"] != "detail-slug" {
		t.Errorf("expected slug=detail-slug, got %v", resp["slug"])
	}
	if resp["fly_machine_id"] != "mach-1" {
		t.Errorf("expected fly_machine_id=mach-1, got %v", resp["fly_machine_id"])
	}
	if resp["wake_schedule"] == nil {
		t.Error("expected wake_schedule to be set")
	}
	if resp["subscription"] == nil {
		t.Error("expected subscription to be set")
	}
	sub := resp["subscription"].(map[string]any)
	if sub["stripe_customer_id"] != "cus_abc" {
		t.Errorf("expected stripe_customer_id=cus_abc, got %v", sub["stripe_customer_id"])
	}
}

func TestHandleTenantDetailNotFound(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	req := authedReq(t, s, http.MethodGet, "/api/tenants/nonexistent", "nonexistent", opToken)
	rec := httptest.NewRecorder()
	s.handleTenantDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleTenantHeartbeats(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)

	tenantID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, jwt_secret) VALUES (?, ?, ?, ?)`,
		tenantID, ownerID, "hb-slug", "secret",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Seed 15 heartbeats.
	for i := 0; i < 15; i++ {
		_, err := s.db.db.Exec(
			`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, ?, ?, ?)`,
			tenantID, i*10, 0.0, float64(i)*10.0,
		)
		if err != nil {
			t.Fatalf("seed heartbeat %d: %v", i, err)
		}
	}

	req := authedReq(t, s, http.MethodGet, "/api/tenants/"+tenantID+"/heartbeats", tenantID, opToken)
	rec := httptest.NewRecorder()
	s.handleTenantHeartbeats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	hbs, ok := resp["heartbeats"].([]any)
	if !ok {
		t.Fatalf("expected heartbeats array, got %T", resp["heartbeats"])
	}
	if len(hbs) != 10 {
		t.Errorf("expected 10 heartbeats (limit), got %d", len(hbs))
	}
}

func TestHandleListReleases(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	releaseID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, frontend_version, severity, components, changelog) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		releaseID, "v1.2.3", "registry.fly.io/app:v1.2.3", "fe-v1.2.3", "minor", "all", "fix: something",
	)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	req := authedReq(t, s, http.MethodGet, "/api/releases", "", opToken)
	rec := httptest.NewRecorder()
	s.handleListReleases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	releases, ok := resp["releases"].([]any)
	if !ok {
		t.Fatalf("expected releases array, got %T", resp["releases"])
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}

	rel := releases[0].(map[string]any)
	if rel["id"] != releaseID {
		t.Errorf("expected id=%s, got %v", releaseID, rel["id"])
	}
	if rel["version"] != "v1.2.3" {
		t.Errorf("expected version=v1.2.3, got %v", rel["version"])
	}
}

func TestHandleListRollouts(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	releaseID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO releases (id, version, image_tag) VALUES (?, ?, ?)`,
		releaseID, "v2.0.0", "registry.fly.io/app:v2.0.0",
	)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	rolloutID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, components) VALUES (?, ?, ?, ?, ?)`,
		rolloutID, releaseID, "completed", "canary", "all",
	)
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	req := authedReq(t, s, http.MethodGet, "/api/rollouts", "", opToken)
	rec := httptest.NewRecorder()
	s.handleListRollouts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	rollouts, ok := resp["rollouts"].([]any)
	if !ok {
		t.Fatalf("expected rollouts array, got %T", resp["rollouts"])
	}
	if len(rollouts) != 1 {
		t.Fatalf("expected 1 rollout, got %d", len(rollouts))
	}

	ro := rollouts[0].(map[string]any)
	if ro["id"] != rolloutID {
		t.Errorf("expected id=%s, got %v", rolloutID, ro["id"])
	}
	if ro["release_version"] != "v2.0.0" {
		t.Errorf("expected release_version=v2.0.0, got %v", ro["release_version"])
	}
}

func TestHandleRolloutDetail(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)
	ownerID, _ := seedAccount(t, s, "owner@example.com", false)

	// Seed the full chain: release -> rollout -> batch -> tenant -> rollout_tenant.
	releaseID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO releases (id, version, image_tag) VALUES (?, ?, ?)`,
		releaseID, "v3.0.0", "registry.fly.io/app:v3.0.0",
	)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}

	rolloutID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, previous_image_tag, previous_frontend_version, components) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rolloutID, releaseID, "in_progress", "canary", "old-image", "old-fe", "all",
	)
	if err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	batchID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, ?, ?, ?)`,
		batchID, rolloutID, 1, 10, "completed",
	)
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	tenantID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, jwt_secret) VALUES (?, ?, ?, ?)`,
		tenantID, ownerID, "rollout-tenant-slug", "secret",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	rtID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status, error_message) VALUES (?, ?, ?, ?, ?)`,
		rtID, batchID, tenantID, "healthy", "",
	)
	if err != nil {
		t.Fatalf("seed rollout_tenant: %v", err)
	}

	req := authedReq(t, s, http.MethodGet, "/api/rollouts/"+rolloutID, rolloutID, opToken)
	rec := httptest.NewRecorder()
	s.handleRolloutDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["id"] != rolloutID {
		t.Errorf("expected id=%s, got %v", rolloutID, resp["id"])
	}
	if resp["release_version"] != "v3.0.0" {
		t.Errorf("expected release_version=v3.0.0, got %v", resp["release_version"])
	}
	if resp["previous_image_tag"] != "old-image" {
		t.Errorf("expected previous_image_tag=old-image, got %v", resp["previous_image_tag"])
	}

	batches, ok := resp["batches"].([]any)
	if !ok || len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %v", resp["batches"])
	}

	b := batches[0].(map[string]any)
	if b["id"] != batchID {
		t.Errorf("expected batch id=%s, got %v", batchID, b["id"])
	}

	tenants, ok := b["tenants"].([]any)
	if !ok || len(tenants) != 1 {
		t.Fatalf("expected 1 tenant in batch, got %v", b["tenants"])
	}

	te := tenants[0].(map[string]any)
	if te["tenant_slug"] != "rollout-tenant-slug" {
		t.Errorf("expected tenant_slug=rollout-tenant-slug, got %v", te["tenant_slug"])
	}
	if te["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", te["status"])
	}
}

func TestHandleRolloutDetailNotFound(t *testing.T) {
	s := newOperatorTestServer(t)
	_, opToken := seedAccount(t, s, "op@example.com", true)

	req := authedReq(t, s, http.MethodGet, "/api/rollouts/nonexistent", "nonexistent", opToken)
	rec := httptest.NewRecorder()
	s.handleRolloutDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// Ensure bcrypt import is used.
var _ = bcrypt.DefaultCost
