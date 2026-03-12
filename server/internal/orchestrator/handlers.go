package orchestrator

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

// authRequest is the payload for signup and login.
type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authResponse is returned on successful signup or login.
type authResponse struct {
	Token     string `json:"token"`
	AccountID string `json:"account_id"`
}

// createTenantRequest is the payload for tenant creation.
type createTenantRequest struct {
	Slug          string `json:"slug"`
	Tier          string `json:"tier"`
	AdminEmail    string `json:"admin_email"`
	AdminPassword string `json:"admin_password"`
}

// createTenantResponse is returned on successful tenant creation.
type createTenantResponse struct {
	TenantID string `json:"tenant_id"`
	URL      string `json:"url"`
}

// heartbeatRequest is the payload from tenant machines.
type heartbeatRequest struct {
	TenantID     string  `json:"tenant_id"`
	RequestCount int     `json:"request_count"`
	ErrorRate    float64 `json:"error_rate"`
	P95Latency   float64 `json:"p95_latency_ms"`
}

// scheduleWakeRequest is the payload for scheduling a machine wake.
type scheduleWakeRequest struct {
	TenantID string `json:"tenant_id"`
	WakeAt   string `json:"wake_at"`
}

// newReleaseRequest is the payload for registering a new release.
type newReleaseRequest struct {
	Version         string `json:"version"`
	ImageTag        string `json:"image_tag"`
	FrontendVersion string `json:"frontend_version"`
	Severity        string `json:"severity"`
	Components      string `json:"components"`
	Changelog       string `json:"changelog"`
}

// handleSignup creates a new orchestrator account and returns a JWT.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	// Rate limit signup requests per IP.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if s.signupLimiter != nil && !s.signupLimiter.Allow(ip) {
		w.Header().Set("Retry-After", "6")
		jsonError(w, "too many requests, please try again later", http.StatusTooManyRequests)
		return
	}

	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		jsonError(w, "email and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("signup: bcrypt error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	accountID := ulid.Make().String()
	_, err = s.db.db.Exec(
		`INSERT INTO accounts (id, email, password_hash) VALUES (?, ?, ?)`,
		accountID, req.Email, string(hash),
	)
	if err != nil {
		log.Printf("signup: insert account: %v", err)
		jsonError(w, "signup failed", http.StatusConflict)
		return
	}

	token, err := s.generateToken(accountID, req.Email)
	if err != nil {
		log.Printf("signup: token error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, AccountID: accountID})
}

// handleLogin authenticates an account and returns a JWT.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		jsonError(w, "email and password are required", http.StatusBadRequest)
		return
	}

	var accountID, hash string
	err := s.db.db.QueryRow(
		`SELECT id, password_hash FROM accounts WHERE email = ?`, req.Email,
	).Scan(&accountID, &hash)
	if err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := s.generateToken(accountID, req.Email)
	if err != nil {
		log.Printf("login: token error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, AccountID: accountID})
}

// handleCreateTenant provisions a new tenant for the authenticated account.
func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	accountID := r.Context().Value(ctxAccountID).(string)

	var req createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !slugPattern.MatchString(req.Slug) {
		jsonError(w, "slug must be 3-30 characters, alphanumeric and hyphens, must start and end with alphanumeric", http.StatusBadRequest)
		return
	}
	if _, ok := tiers[req.Tier]; !ok {
		jsonError(w, "tier must be one of: free, starter, pro", http.StatusBadRequest)
		return
	}
	if req.AdminEmail == "" || req.AdminPassword == "" {
		jsonError(w, "admin_email and admin_password are required", http.StatusBadRequest)
		return
	}

	result, err := s.provisioner.Provision(ProvisionRequest{
		AccountID:     accountID,
		Slug:          req.Slug,
		Tier:          req.Tier,
		AdminEmail:    req.AdminEmail,
		AdminPassword: req.AdminPassword,
	})
	if err != nil {
		log.Printf("create tenant: %v", err)
		jsonError(w, "failed to provision tenant", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, createTenantResponse{
		TenantID: result.TenantID,
		URL:      result.URL,
	})
}

// handleDeleteTenant tears down a tenant after verifying ownership.
func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	accountID := r.Context().Value(ctxAccountID).(string)
	tenantID := r.PathValue("id")

	// Verify ownership.
	var ownerID string
	err := s.db.db.QueryRow(
		`SELECT account_id FROM tenants WHERE id = ?`, tenantID,
	).Scan(&ownerID)
	if err != nil {
		jsonError(w, "tenant not found", http.StatusNotFound)
		return
	}
	isOp, _ := r.Context().Value(ctxIsOperator).(bool)
	if ownerID != accountID && !isOp {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := s.provisioner.Deprovision(tenantID); err != nil {
		log.Printf("delete tenant: %v", err)
		jsonError(w, "failed to deprovision tenant", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTenantStatus returns the current status and tier of a tenant.
func (s *Server) handleTenantStatus(w http.ResponseWriter, r *http.Request) {
	accountID := r.Context().Value(ctxAccountID).(string)
	tenantID := r.PathValue("id")

	var ownerID, slug, tier, status string
	err := s.db.db.QueryRow(
		`SELECT account_id, slug, tier, status FROM tenants WHERE id = ?`, tenantID,
	).Scan(&ownerID, &slug, &tier, &status)
	if err != nil {
		jsonError(w, "tenant not found", http.StatusNotFound)
		return
	}
	isOp, _ := r.Context().Value(ctxIsOperator).(bool)
	if ownerID != accountID && !isOp {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"tenant_id": tenantID,
		"slug":      slug,
		"tier":      tier,
		"status":    status,
	})
}

// handleHeartbeat records a heartbeat from a tenant machine.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TenantID == "" {
		jsonError(w, "tenant_id is required", http.StatusBadRequest)
		return
	}

	_, err := s.db.db.Exec(
		`INSERT INTO tenant_heartbeats (tenant_id, request_count, error_rate, p95_latency_ms) VALUES (?, ?, ?, ?)`,
		req.TenantID, req.RequestCount, req.ErrorRate, req.P95Latency,
	)
	if err != nil {
		log.Printf("heartbeat: %v", err)
		jsonError(w, "failed to record heartbeat", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleScheduleWake upserts a wake time for a tenant machine.
func (s *Server) handleScheduleWake(w http.ResponseWriter, r *http.Request) {
	var req scheduleWakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TenantID == "" || req.WakeAt == "" {
		jsonError(w, "tenant_id and wake_at are required", http.StatusBadRequest)
		return
	}

	// Validate the timestamp format.
	if _, err := time.Parse(time.RFC3339, req.WakeAt); err != nil {
		jsonError(w, "wake_at must be RFC3339 format", http.StatusBadRequest)
		return
	}

	_, err := s.db.db.Exec(
		`INSERT OR REPLACE INTO wake_schedule (tenant_id, wake_at) VALUES (?, ?)`,
		req.TenantID, req.WakeAt,
	)
	if err != nil {
		log.Printf("schedule-wake: %v", err)
		jsonError(w, "failed to schedule wake", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleNewRelease stores a new release record.
func (s *Server) handleNewRelease(w http.ResponseWriter, r *http.Request) {
	var req newReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Version == "" || req.ImageTag == "" {
		jsonError(w, "version and image_tag are required", http.StatusBadRequest)
		return
	}

	releaseID := ulid.Make().String()
	_, err := s.db.db.Exec(
		`INSERT INTO releases (id, version, image_tag, frontend_version, severity, components, changelog) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		releaseID, req.Version, req.ImageTag, req.FrontendVersion, req.Severity, req.Components, req.Changelog,
	)
	if err != nil {
		log.Printf("new-release: %v", err)
		jsonError(w, "failed to store release", http.StatusInternalServerError)
		return
	}

	// Trigger rollout (StartRollout launches its own background goroutine).
	if err := s.rolloutManager.StartRollout(releaseID); err != nil {
		log.Printf("new-release: rollout start failed: %v", err)
	}

	writeJSON(w, http.StatusCreated, map[string]string{"release_id": releaseID})
}

// handleRollback reverts a rollout to the previous image version.
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	rolloutID := r.PathValue("id")
	if err := s.rolloutManager.Rollback(rolloutID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "rolled_back"})
}

// handleListTenants returns all non-deleted tenants with their latest heartbeat metrics.
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.db.Query(`
		SELECT t.id, t.slug, t.tier, t.status, t.created_at,
			h.received_at, h.request_count, h.error_rate, h.p95_latency_ms
		FROM tenants t
		LEFT JOIN tenant_heartbeats h ON h.id = (
			SELECT id FROM tenant_heartbeats WHERE tenant_id = t.id ORDER BY received_at DESC LIMIT 1
		)
		WHERE t.status != 'deleted'
		ORDER BY t.created_at DESC
	`)
	if err != nil {
		jsonError(w, "failed to query tenants", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type tenantRow struct {
		ID              string   `json:"id"`
		Slug            string   `json:"slug"`
		Tier            string   `json:"tier"`
		Status          string   `json:"status"`
		CreatedAt       string   `json:"created_at"`
		LastHeartbeatAt *string  `json:"last_heartbeat_at"`
		RequestCount    *int     `json:"request_count"`
		ErrorRate       *float64 `json:"error_rate"`
		P95Latency      *float64 `json:"p95_latency_ms"`
	}

	var tenants []tenantRow
	for rows.Next() {
		var t tenantRow
		if err := rows.Scan(&t.ID, &t.Slug, &t.Tier, &t.Status, &t.CreatedAt,
			&t.LastHeartbeatAt, &t.RequestCount, &t.ErrorRate, &t.P95Latency); err != nil {
			jsonError(w, "failed to scan tenant", http.StatusInternalServerError)
			return
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		jsonError(w, "failed to iterate tenants", http.StatusInternalServerError)
		return
	}
	if tenants == nil {
		tenants = []tenantRow{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tenants": tenants,
		"total":   len(tenants),
	})
}

// handleFleetStats returns aggregate tenant counts by status plus any active rollout.
func (s *Server) handleFleetStats(w http.ResponseWriter, r *http.Request) {
	var total, active, sleeping, errored, provisioning int
	row := s.db.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'sleeping' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'provisioning' THEN 1 ELSE 0 END), 0)
		FROM tenants WHERE status != 'deleted'
	`)
	if err := row.Scan(&total, &active, &sleeping, &errored, &provisioning); err != nil {
		jsonError(w, "failed to query fleet stats", http.StatusInternalServerError)
		return
	}

	var activeRollout any
	var rolloutID, rolloutVersion, strategy string
	err := s.db.db.QueryRow(`
		SELECT ro.id, rel.version, ro.strategy
		FROM rollouts ro
		JOIN releases rel ON rel.id = ro.release_id
		WHERE ro.status = 'in_progress'
		ORDER BY ro.created_at DESC LIMIT 1
	`).Scan(&rolloutID, &rolloutVersion, &strategy)
	if err == nil {
		var totalTenants, healthyTenants int
		s.db.db.QueryRow(`
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN rt.status = 'healthy' THEN 1 ELSE 0 END), 0)
			FROM rollout_tenants rt
			JOIN rollout_batches rb ON rb.id = rt.rollout_batch_id
			WHERE rb.rollout_id = ?
		`, rolloutID).Scan(&totalTenants, &healthyTenants)

		pct := 0
		if totalTenants > 0 {
			pct = healthyTenants * 100 / totalTenants
		}
		activeRollout = map[string]any{
			"id":           rolloutID,
			"version":      rolloutVersion,
			"strategy":     strategy,
			"progress_pct": pct,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":          total,
		"active":         active,
		"sleeping":       sleeping,
		"error":          errored,
		"provisioning":   provisioning,
		"active_rollout": activeRollout,
	})
}

// handleTenantDetail returns full tenant info including wake schedule and subscription.
func (s *Server) handleTenantDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")

	var id, slug, tier, status, flyMachineID, flyVolumeID, createdAt, updatedAt string
	err := s.db.db.QueryRow(`
		SELECT id, slug, tier, status, fly_machine_id, fly_volume_id, created_at, updated_at
		FROM tenants WHERE id = ?
	`, tenantID).Scan(&id, &slug, &tier, &status, &flyMachineID, &flyVolumeID, &createdAt, &updatedAt)
	if err != nil {
		jsonError(w, "tenant not found", http.StatusNotFound)
		return
	}

	resp := map[string]any{
		"id":             id,
		"slug":           slug,
		"tier":           tier,
		"status":         status,
		"fly_machine_id": flyMachineID,
		"fly_volume_id":  flyVolumeID,
		"created_at":     createdAt,
		"updated_at":     updatedAt,
		"url":            "https://" + slug + ".cogitator.cloud",
		"wake_schedule":  nil,
		"subscription":   nil,
	}

	var wakeAt string
	if err := s.db.db.QueryRow(`SELECT wake_at FROM wake_schedule WHERE tenant_id = ?`, tenantID).Scan(&wakeAt); err == nil {
		resp["wake_schedule"] = wakeAt
	}

	var subCustID, subTier, subStatus string
	var subPeriodEnd *string
	if err := s.db.db.QueryRow(`
		SELECT stripe_customer_id, tier, status, current_period_end
		FROM subscriptions WHERE tenant_id = ?
	`, tenantID).Scan(&subCustID, &subTier, &subStatus, &subPeriodEnd); err == nil {
		resp["subscription"] = map[string]any{
			"stripe_customer_id": subCustID,
			"tier":               subTier,
			"status":             subStatus,
			"current_period_end": subPeriodEnd,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleTenantHeartbeats returns the last 10 heartbeats for a tenant.
func (s *Server) handleTenantHeartbeats(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")

	rows, err := s.db.db.Query(`
		SELECT id, request_count, error_rate, p95_latency_ms, received_at
		FROM tenant_heartbeats
		WHERE tenant_id = ?
		ORDER BY received_at DESC
		LIMIT 10
	`, tenantID)
	if err != nil {
		jsonError(w, "failed to query heartbeats", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type hb struct {
		ID           int     `json:"id"`
		RequestCount int     `json:"request_count"`
		ErrorRate    float64 `json:"error_rate"`
		P95Latency   float64 `json:"p95_latency_ms"`
		ReceivedAt   string  `json:"received_at"`
	}
	var heartbeats []hb
	for rows.Next() {
		var h hb
		if err := rows.Scan(&h.ID, &h.RequestCount, &h.ErrorRate, &h.P95Latency, &h.ReceivedAt); err != nil {
			jsonError(w, "failed to scan heartbeat", http.StatusInternalServerError)
			return
		}
		heartbeats = append(heartbeats, h)
	}
	if err := rows.Err(); err != nil {
		jsonError(w, "failed to iterate heartbeats", http.StatusInternalServerError)
		return
	}
	if heartbeats == nil {
		heartbeats = []hb{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"heartbeats": heartbeats})
}

// handleListReleases returns all releases ordered by created_at descending.
func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.db.Query(`
		SELECT id, version, image_tag, frontend_version, severity, components, changelog, created_at
		FROM releases ORDER BY created_at DESC
	`)
	if err != nil {
		jsonError(w, "failed to query releases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type release struct {
		ID              string `json:"id"`
		Version         string `json:"version"`
		ImageTag        string `json:"image_tag"`
		FrontendVersion string `json:"frontend_version"`
		Severity        string `json:"severity"`
		Components      string `json:"components"`
		Changelog       string `json:"changelog"`
		CreatedAt       string `json:"created_at"`
	}
	var releases []release
	for rows.Next() {
		var rel release
		if err := rows.Scan(&rel.ID, &rel.Version, &rel.ImageTag, &rel.FrontendVersion, &rel.Severity, &rel.Components, &rel.Changelog, &rel.CreatedAt); err != nil {
			jsonError(w, "failed to scan release", http.StatusInternalServerError)
			return
		}
		releases = append(releases, rel)
	}
	if err := rows.Err(); err != nil {
		jsonError(w, "failed to iterate releases", http.StatusInternalServerError)
		return
	}
	if releases == nil {
		releases = []release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

// handleListRollouts returns all rollouts joined with release version and progress.
func (s *Server) handleListRollouts(w http.ResponseWriter, r *http.Request) {
	type rollout struct {
		ID             string `json:"id"`
		ReleaseVersion string `json:"release_version"`
		Status         string `json:"status"`
		Strategy       string `json:"strategy"`
		Components     string `json:"components"`
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
		ProgressPct    int    `json:"progress_pct"`
	}

	// Collect all rollouts first, then close the cursor before issuing sub-queries.
	// The DB has MaxOpenConns=1, so nesting queries inside an open rows iterator
	// would deadlock.
	var rollouts []rollout
	{
		rows, err := s.db.db.Query(`
			SELECT ro.id, rel.version, ro.status, ro.strategy, ro.components, ro.created_at, ro.updated_at
			FROM rollouts ro
			JOIN releases rel ON rel.id = ro.release_id
			ORDER BY ro.created_at DESC
		`)
		if err != nil {
			jsonError(w, "failed to query rollouts", http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var ro rollout
			if err := rows.Scan(&ro.ID, &ro.ReleaseVersion, &ro.Status, &ro.Strategy, &ro.Components, &ro.CreatedAt, &ro.UpdatedAt); err != nil {
				rows.Close()
				jsonError(w, "failed to scan rollout", http.StatusInternalServerError)
				return
			}
			rollouts = append(rollouts, ro)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			jsonError(w, "failed to iterate rollouts", http.StatusInternalServerError)
			return
		}
		rows.Close()
	}

	// Now safe to issue per-rollout progress sub-queries.
	for i := range rollouts {
		var total, healthy int
		s.db.db.QueryRow(`
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN rt.status = 'healthy' THEN 1 ELSE 0 END), 0)
			FROM rollout_tenants rt
			JOIN rollout_batches rb ON rb.id = rt.rollout_batch_id
			WHERE rb.rollout_id = ?
		`, rollouts[i].ID).Scan(&total, &healthy)
		if total > 0 {
			rollouts[i].ProgressPct = healthy * 100 / total
		}
	}

	if rollouts == nil {
		rollouts = []rollout{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rollouts": rollouts})
}

// handleRolloutDetail returns a rollout with nested batches and per-batch tenant list.
func (s *Server) handleRolloutDetail(w http.ResponseWriter, r *http.Request) {
	rolloutID := r.PathValue("id")

	var roID, relVersion, status, strategy, prevImage, prevFrontend, components, createdAt, updatedAt string
	err := s.db.db.QueryRow(`
		SELECT ro.id, rel.version, ro.status, ro.strategy, ro.previous_image_tag,
			ro.previous_frontend_version, ro.components, ro.created_at, ro.updated_at
		FROM rollouts ro
		JOIN releases rel ON rel.id = ro.release_id
		WHERE ro.id = ?
	`, rolloutID).Scan(&roID, &relVersion, &status, &strategy, &prevImage, &prevFrontend, &components, &createdAt, &updatedAt)
	if err != nil {
		jsonError(w, "rollout not found", http.StatusNotFound)
		return
	}

	type tenantEntry struct {
		ID              string  `json:"id"`
		TenantID        string  `json:"tenant_id"`
		TenantSlug      string  `json:"tenant_slug"`
		Status          string  `json:"status"`
		HealthCheckedAt *string `json:"health_checked_at"`
		ErrorMessage    string  `json:"error_message"`
	}
	type batch struct {
		ID          string        `json:"id"`
		BatchNumber int           `json:"batch_number"`
		Percentage  int           `json:"percentage"`
		Status      string        `json:"status"`
		StartedAt   *string       `json:"started_at"`
		CompletedAt *string       `json:"completed_at"`
		Tenants     []tenantEntry `json:"tenants"`
	}

	// Collect batches first, then close the cursor before issuing per-batch
	// tenant sub-queries. The DB has MaxOpenConns=1, so nesting queries inside
	// an open rows iterator would deadlock.
	var batches []batch
	{
		batchRows, err := s.db.db.Query(`
			SELECT id, batch_number, percentage, status, started_at, completed_at
			FROM rollout_batches WHERE rollout_id = ? ORDER BY batch_number
		`, rolloutID)
		if err != nil {
			jsonError(w, "failed to query batches", http.StatusInternalServerError)
			return
		}
		for batchRows.Next() {
			var b batch
			if err := batchRows.Scan(&b.ID, &b.BatchNumber, &b.Percentage, &b.Status, &b.StartedAt, &b.CompletedAt); err != nil {
				batchRows.Close()
				jsonError(w, "failed to scan batch", http.StatusInternalServerError)
				return
			}
			batches = append(batches, b)
		}
		if err := batchRows.Err(); err != nil {
			batchRows.Close()
			jsonError(w, "failed to iterate batches", http.StatusInternalServerError)
			return
		}
		batchRows.Close()
	}

	// Now safe to query tenants per batch.
	for i := range batches {
		tRows, err := s.db.db.Query(`
			SELECT rt.id, rt.tenant_id, t.slug, rt.status, rt.health_checked_at, rt.error_message
			FROM rollout_tenants rt
			JOIN tenants t ON t.id = rt.tenant_id
			WHERE rt.rollout_batch_id = ?
		`, batches[i].ID)
		if err != nil {
			jsonError(w, "failed to query rollout tenants", http.StatusInternalServerError)
			return
		}
		for tRows.Next() {
			var te tenantEntry
			if err := tRows.Scan(&te.ID, &te.TenantID, &te.TenantSlug, &te.Status, &te.HealthCheckedAt, &te.ErrorMessage); err != nil {
				tRows.Close()
				jsonError(w, "failed to scan rollout tenant", http.StatusInternalServerError)
				return
			}
			batches[i].Tenants = append(batches[i].Tenants, te)
		}
		if err := tRows.Err(); err != nil {
			tRows.Close()
			jsonError(w, "failed to iterate rollout tenants", http.StatusInternalServerError)
			return
		}
		tRows.Close()
		if batches[i].Tenants == nil {
			batches[i].Tenants = []tenantEntry{}
		}
	}
	if batches == nil {
		batches = []batch{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                        roID,
		"release_version":           relVersion,
		"status":                    status,
		"strategy":                  strategy,
		"previous_image_tag":        prevImage,
		"previous_frontend_version": prevFrontend,
		"components":                components,
		"created_at":                createdAt,
		"updated_at":                updatedAt,
		"batches":                   batches,
	})
}

// writeJSON serializes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
