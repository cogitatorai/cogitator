package orchestrator

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator/fly"
	"github.com/oklog/ulid/v2"
)

// RolloutManager handles safely updating tenant machines using canary
// or fleet-wide strategies. Canary rolls out in three phases (5%, 25%, 100%)
// with validation between each. Fleet-wide updates all tenants in a single batch.
type RolloutManager struct {
	db     *OrchestratorDB
	fly    fly.MachineAPI
	client *http.Client
	secret string
	wg     sync.WaitGroup

	// Configurable timing (overridden in tests).
	canaryWait          time.Duration
	drainPollInterval   time.Duration
	healthCheckInterval time.Duration
	drainTimeout        time.Duration
	healthCheckTimeout  time.Duration
}

// NewRolloutManager returns a RolloutManager with production defaults.
func NewRolloutManager(db *OrchestratorDB, flyClient fly.MachineAPI, secret string) *RolloutManager {
	return &RolloutManager{
		db:                  db,
		fly:                 flyClient,
		client:              &http.Client{Timeout: 30 * time.Second},
		secret:              secret,
		canaryWait:          15 * time.Minute,
		drainPollInterval:   5 * time.Second,
		healthCheckInterval: 2 * time.Second,
		drainTimeout:        10 * time.Minute,
		healthCheckTimeout:  60 * time.Second,
	}
}

// release holds the fields read from the releases table.
type release struct {
	ID        string
	Version   string
	ImageTag  string
	Severity  string
	Components string
}

// activeTenant represents a tenant eligible for rollout.
type activeTenant struct {
	ID        string
	MachineID string
	Slug      string
}

// StartRollout creates a rollout for the given release and executes it.
// It determines the strategy from severity: patch uses fleet-wide,
// minor/major use canary.
func (rm *RolloutManager) StartRollout(releaseID string) error {
	// 1. Load the release.
	rel, err := rm.loadRelease(releaseID)
	if err != nil {
		return fmt.Errorf("rollout: load release: %w", err)
	}

	// 2. Determine strategy.
	strategy := "canary"
	if rel.Severity == "patch" {
		strategy = "fleet_wide"
	}

	// 3. Get previous image tag for rollback reference.
	previousImage := rm.previousImageTag(releaseID)

	// 4. Create rollout record.
	rolloutID := ulid.Make().String()
	now := time.Now().UTC().Format(time.DateTime)
	_, err = rm.db.db.Exec(
		`INSERT INTO rollouts (id, release_id, status, strategy, previous_image_tag, components, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, ?, ?, ?, ?)`,
		rolloutID, releaseID, strategy, previousImage, rel.Components, now, now,
	)
	if err != nil {
		return fmt.Errorf("rollout: create rollout: %w", err)
	}

	// 5. Get active tenants.
	tenants, err := rm.activeTenants()
	if err != nil {
		return fmt.Errorf("rollout: list tenants: %w", err)
	}
	if len(tenants) == 0 {
		rm.setRolloutStatus(rolloutID, "completed")
		return nil
	}

	// 6. Create batches.
	if err := rm.createBatches(rolloutID, strategy, tenants); err != nil {
		return fmt.Errorf("rollout: create batches: %w", err)
	}

	// 7. Execute in background.
	rm.wg.Add(1)
	go func() {
		defer rm.wg.Done()
		rm.executeRollout(rolloutID, rel.ImageTag, rel.Components)
	}()

	return nil
}

// Shutdown blocks until all in-flight rollouts complete.
func (rm *RolloutManager) Shutdown() {
	rm.wg.Wait()
}

// loadRelease reads a release record from the database.
func (rm *RolloutManager) loadRelease(id string) (*release, error) {
	var r release
	err := rm.db.db.QueryRow(
		`SELECT id, version, image_tag, severity, components FROM releases WHERE id = ?`, id,
	).Scan(&r.ID, &r.Version, &r.ImageTag, &r.Severity, &r.Components)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// previousImageTag returns the image tag of the release created immediately
// before the given one, or an empty string if none exists.
func (rm *RolloutManager) previousImageTag(releaseID string) string {
	var tag string
	_ = rm.db.db.QueryRow(
		`SELECT image_tag FROM releases WHERE created_at < (SELECT created_at FROM releases WHERE id = ?)
		 ORDER BY created_at DESC LIMIT 1`, releaseID,
	).Scan(&tag)
	return tag
}

// activeTenants returns all tenants with status "active".
func (rm *RolloutManager) activeTenants() ([]activeTenant, error) {
	rows, err := rm.db.db.Query(
		`SELECT id, fly_machine_id, slug FROM tenants WHERE status = 'active'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []activeTenant
	for rows.Next() {
		var t activeTenant
		if err := rows.Scan(&t.ID, &t.MachineID, &t.Slug); err != nil {
			return nil, err
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// createBatches builds rollout_batches and rollout_tenants rows.
// Fleet-wide: 1 batch at 100%. Canary: 3 batches at 5%, 25%, 100%.
func (rm *RolloutManager) createBatches(rolloutID, strategy string, tenants []activeTenant) error {
	type batchDef struct {
		number     int
		percentage int
	}

	var batches []batchDef
	if strategy == "fleet_wide" {
		batches = []batchDef{{1, 100}}
	} else {
		batches = []batchDef{{1, 5}, {2, 25}, {3, 100}}
	}

	total := len(tenants)
	assigned := 0

	for i, bd := range batches {
		batchID := ulid.Make().String()
		_, err := rm.db.db.Exec(
			`INSERT INTO rollout_batches (id, rollout_id, batch_number, percentage, status) VALUES (?, ?, ?, ?, 'pending')`,
			batchID, rolloutID, bd.number, bd.percentage,
		)
		if err != nil {
			return err
		}

		// Calculate how many tenants go in this batch.
		var count int
		if i == len(batches)-1 {
			// Last batch gets the remainder.
			count = total - assigned
		} else {
			count = int(math.Ceil(float64(total) * float64(bd.percentage) / 100.0))
			if count < 1 {
				count = 1
			}
			// Don't exceed remaining tenants.
			if count > total-assigned {
				count = total - assigned
			}
		}

		for j := 0; j < count; j++ {
			tenantIdx := assigned + j
			rtID := ulid.Make().String()
			_, err := rm.db.db.Exec(
				`INSERT INTO rollout_tenants (id, rollout_batch_id, tenant_id, status) VALUES (?, ?, ?, 'pending')`,
				rtID, batchID, tenants[tenantIdx].ID,
			)
			if err != nil {
				return err
			}
		}
		assigned += count
	}
	return nil
}

// executeRollout processes each batch sequentially, validating canary
// metrics between batches.
func (rm *RolloutManager) executeRollout(rolloutID, imageTag, components string) {
	batches, err := rm.loadBatches(rolloutID)
	if err != nil {
		log.Printf("rollout %s: load batches: %v", rolloutID, err)
		rm.setRolloutStatus(rolloutID, "failed")
		return
	}

	for i, batch := range batches {
		// Mark batch as in_progress.
		rm.setBatchStatus(batch.id, "in_progress")

		if err := rm.executeBatch(batch.id, imageTag, components); err != nil {
			log.Printf("rollout %s: batch %d failed: %v", rolloutID, batch.number, err)
			rm.setBatchStatus(batch.id, "failed")
			rm.setRolloutStatus(rolloutID, "paused")
			return
		}

		rm.setBatchStatus(batch.id, "completed")

		// If not the last batch, wait and validate.
		if i < len(batches)-1 {
			if rm.canaryWait > 0 {
				time.Sleep(rm.canaryWait)
			}
			if !rm.validateCanary(batch.id) {
				log.Printf("rollout %s: canary validation failed after batch %d", rolloutID, batch.number)
				rm.setRolloutStatus(rolloutID, "paused")
				return
			}
		}
	}

	rm.setRolloutStatus(rolloutID, "completed")
}

type batchInfo struct {
	id     string
	number int
}

// loadBatches returns all batches for a rollout, ordered by batch_number.
func (rm *RolloutManager) loadBatches(rolloutID string) ([]batchInfo, error) {
	rows, err := rm.db.db.Query(
		`SELECT id, batch_number FROM rollout_batches WHERE rollout_id = ? ORDER BY batch_number`, rolloutID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var batches []batchInfo
	for rows.Next() {
		var b batchInfo
		if err := rows.Scan(&b.id, &b.number); err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, rows.Err()
}

// executeBatch updates every tenant in the batch: drain, update image, start, health check.
func (rm *RolloutManager) executeBatch(batchID, imageTag, components string) error {
	rows, err := rm.db.db.Query(
		`SELECT rt.id, rt.tenant_id, t.fly_machine_id, t.slug
		 FROM rollout_tenants rt
		 JOIN tenants t ON t.id = rt.tenant_id
		 WHERE rt.rollout_batch_id = ?`, batchID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type tenantWork struct {
		rtID      string
		tenantID  string
		machineID string
		slug      string
	}
	var work []tenantWork
	for rows.Next() {
		var tw tenantWork
		if err := rows.Scan(&tw.rtID, &tw.tenantID, &tw.machineID, &tw.slug); err != nil {
			return err
		}
		work = append(work, tw)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, tw := range work {
		baseURL := fmt.Sprintf("https://%s.cogitator.cloud", tw.slug)

		// 1. Drain.
		if err := rm.drainTenant(baseURL); err != nil {
			log.Printf("rollout: drain %s warning: %v (proceeding)", tw.slug, err)
			// Drain timeout is a safety valve; proceed anyway.
		}

		// 2. Update machine image.
		if err := rm.fly.UpdateMachine(tw.machineID, imageTag); err != nil {
			rm.setTenantError(tw.rtID, fmt.Sprintf("update machine: %v", err))
			return fmt.Errorf("update machine %s: %w", tw.machineID, err)
		}

		// 3. Start machine.
		if err := rm.fly.StartMachine(tw.machineID); err != nil {
			rm.setTenantError(tw.rtID, fmt.Sprintf("start machine: %v", err))
			return fmt.Errorf("start machine %s: %w", tw.machineID, err)
		}

		// 4. Health check.
		if err := rm.waitForHealth(baseURL, rm.healthCheckTimeout); err != nil {
			rm.setTenantError(tw.rtID, fmt.Sprintf("health check: %v", err))
			return fmt.Errorf("health check %s: %w", tw.slug, err)
		}

		// 5. Mark tenant as healthy.
		now := time.Now().UTC().Format(time.DateTime)
		_, _ = rm.db.db.Exec(
			`UPDATE rollout_tenants SET status = 'healthy', health_checked_at = ? WHERE id = ?`,
			now, tw.rtID,
		)
	}

	return nil
}

// drainTenant POSTs to the tenant drain endpoint and polls until drained.
func (rm *RolloutManager) drainTenant(baseURL string) error {
	url := baseURL + "/api/internal/drain"

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Secret", rm.secret)

	resp, err := rm.client.Do(req)
	if err != nil {
		return fmt.Errorf("drain request: %w", err)
	}
	resp.Body.Close()

	// Poll until drained.
	deadline := time.Now().Add(rm.drainTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(rm.drainPollInterval)

		r, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		r.Header.Set("X-Internal-Secret", rm.secret)

		resp, err := rm.client.Do(r)
		if err != nil {
			continue
		}

		var result struct {
			Drained bool `json:"drained"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Drained {
			return nil
		}
	}

	return fmt.Errorf("drain timeout after %v", rm.drainTimeout)
}

// waitForHealth polls the tenant health endpoint until it returns 200 OK.
func (rm *RolloutManager) waitForHealth(baseURL string, timeout time.Duration) error {
	url := baseURL + "/api/health"
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := rm.client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(rm.healthCheckInterval)
	}

	return fmt.Errorf("health check timeout after %v", timeout)
}

// validateCanary checks that tenants in the batch are not degraded compared
// to their baseline metrics. Returns true if all pass (or no baseline exists).
func (rm *RolloutManager) validateCanary(batchID string) bool {
	rows, err := rm.db.db.Query(
		`SELECT rt.tenant_id FROM rollout_tenants rt WHERE rt.rollout_batch_id = ?`, batchID,
	)
	if err != nil {
		log.Printf("canary validation: query tenants: %v", err)
		return true // No data means assume OK.
	}
	defer rows.Close()

	var tenantIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return true
		}
		tenantIDs = append(tenantIDs, id)
	}

	for _, tid := range tenantIDs {
		// Get baseline.
		var baseP95 float64
		var baseErr float64
		err := rm.db.db.QueryRow(
			`SELECT p95_latency_ms, error_rate FROM tenant_metrics_baseline WHERE tenant_id = ?`, tid,
		).Scan(&baseP95, &baseErr)
		if err != nil {
			continue // No baseline: assume OK.
		}

		// Get latest heartbeat.
		var curP95 float64
		var curErr float64
		err = rm.db.db.QueryRow(
			`SELECT p95_latency_ms, error_rate FROM tenant_heartbeats WHERE tenant_id = ? ORDER BY received_at DESC LIMIT 1`, tid,
		).Scan(&curP95, &curErr)
		if err != nil {
			continue // No heartbeat: assume OK.
		}

		// Check thresholds.
		if curP95 > baseP95*1.5 {
			log.Printf("canary validation: tenant %s p95 %.1fms exceeds baseline %.1fms * 1.5", tid, curP95, baseP95)
			return false
		}
		if curErr > baseErr+0.02 {
			log.Printf("canary validation: tenant %s error rate %.4f exceeds baseline %.4f + 0.02", tid, curErr, baseErr)
			return false
		}
	}

	return true
}

// Rollback reverts all healthy tenants in a rollout to the previous image.
// It loads the rollout's previous_image_tag, finds all rollout_tenants marked
// as 'healthy', and for each one: drains, updates the machine image back to
// the previous version, starts the machine, and verifies health. Each tenant
// is marked 'rolled_back' on success. The rollout itself is marked 'rolled_back'
// when all tenants are processed.
func (rm *RolloutManager) Rollback(rolloutID string) error {
	// 1. Load rollout metadata.
	var previousImage, components string
	err := rm.db.db.QueryRow(
		`SELECT previous_image_tag, components FROM rollouts WHERE id = ?`, rolloutID,
	).Scan(&previousImage, &components)
	if err != nil {
		return fmt.Errorf("rollback: load rollout %s: %w", rolloutID, err)
	}
	if previousImage == "" {
		return fmt.Errorf("rollback: no previous image tag for rollout %s", rolloutID)
	}

	// 2. Find all healthy tenants for this rollout.
	rows, err := rm.db.db.Query(
		`SELECT rt.id, rt.tenant_id, t.fly_machine_id, t.slug
		 FROM rollout_tenants rt
		 JOIN rollout_batches rb ON rb.id = rt.rollout_batch_id
		 JOIN tenants t ON t.id = rt.tenant_id
		 WHERE rb.rollout_id = ? AND rt.status = 'healthy'`, rolloutID,
	)
	if err != nil {
		return fmt.Errorf("rollback: query tenants: %w", err)
	}
	defer rows.Close()

	type rollbackTarget struct {
		rtID      string
		tenantID  string
		machineID string
		slug      string
	}
	var targets []rollbackTarget
	for rows.Next() {
		var t rollbackTarget
		if err := rows.Scan(&t.rtID, &t.tenantID, &t.machineID, &t.slug); err != nil {
			return fmt.Errorf("rollback: scan tenant: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rollback: iterate tenants: %w", err)
	}

	// 3. Revert each tenant: drain, update to previous image, start, health check.
	for _, tgt := range targets {
		baseURL := fmt.Sprintf("https://%s.cogitator.cloud", tgt.slug)

		if err := rm.drainTenant(baseURL); err != nil {
			log.Printf("rollback: drain %s warning: %v (proceeding)", tgt.slug, err)
		}

		if err := rm.fly.UpdateMachine(tgt.machineID, previousImage); err != nil {
			rm.setTenantError(tgt.rtID, fmt.Sprintf("rollback update machine: %v", err))
			return fmt.Errorf("rollback: update machine %s: %w", tgt.machineID, err)
		}

		if err := rm.fly.StartMachine(tgt.machineID); err != nil {
			rm.setTenantError(tgt.rtID, fmt.Sprintf("rollback start machine: %v", err))
			return fmt.Errorf("rollback: start machine %s: %w", tgt.machineID, err)
		}

		if err := rm.waitForHealth(baseURL, rm.healthCheckTimeout); err != nil {
			rm.setTenantError(tgt.rtID, fmt.Sprintf("rollback health check: %v", err))
			return fmt.Errorf("rollback: health check %s: %w", tgt.slug, err)
		}

		now := time.Now().UTC().Format(time.DateTime)
		_, _ = rm.db.db.Exec(
			`UPDATE rollout_tenants SET status = 'rolled_back', health_checked_at = ? WHERE id = ?`,
			now, tgt.rtID,
		)
	}

	// 4. Mark rollout as rolled_back.
	rm.setRolloutStatus(rolloutID, "rolled_back")
	return nil
}

// setRolloutStatus updates the rollout status and updated_at timestamp.
func (rm *RolloutManager) setRolloutStatus(rolloutID, status string) {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := rm.db.db.Exec(
		`UPDATE rollouts SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, rolloutID,
	)
	if err != nil {
		log.Printf("rollout: set status %s for %s: %v", status, rolloutID, err)
	}
}

// setBatchStatus updates a batch status and timestamps.
func (rm *RolloutManager) setBatchStatus(batchID, status string) {
	now := time.Now().UTC().Format(time.DateTime)
	switch status {
	case "in_progress":
		_, _ = rm.db.db.Exec(
			`UPDATE rollout_batches SET status = ?, started_at = ? WHERE id = ?`,
			status, now, batchID,
		)
	case "completed", "failed":
		_, _ = rm.db.db.Exec(
			`UPDATE rollout_batches SET status = ?, completed_at = ? WHERE id = ?`,
			status, now, batchID,
		)
	default:
		_, _ = rm.db.db.Exec(
			`UPDATE rollout_batches SET status = ? WHERE id = ?`,
			status, batchID,
		)
	}
}

// setTenantError records an error message on a rollout_tenants row.
func (rm *RolloutManager) setTenantError(rtID, msg string) {
	_, _ = rm.db.db.Exec(
		`UPDATE rollout_tenants SET status = 'failed', error_message = ? WHERE id = ?`,
		msg, rtID,
	)
}
