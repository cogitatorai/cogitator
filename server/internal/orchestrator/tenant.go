package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator/cloudflare"
	"github.com/cogitatorai/cogitator/server/internal/orchestrator/fly"
	"github.com/oklog/ulid/v2"
)

// TenantProvisioner orchestrates the creation and teardown of tenant
// infrastructure: Fly volume, Fly machine, DNS record, and DB state.
type TenantProvisioner struct {
	db         *OrchestratorDB
	fly        fly.MachineAPI
	cf         cloudflare.DNSAPI
	region     string
	imageTag   string
	secret     string
	flyAppName string
}

// NewTenantProvisioner returns a provisioner wired to the given backends.
func NewTenantProvisioner(db *OrchestratorDB, f fly.MachineAPI, cf cloudflare.DNSAPI, region, imageTag, secret, flyAppName string) *TenantProvisioner {
	return &TenantProvisioner{
		db:         db,
		fly:        f,
		cf:         cf,
		region:     region,
		imageTag:   imageTag,
		secret:     secret,
		flyAppName: flyAppName,
	}
}

// ProvisionRequest describes a new tenant to create.
type ProvisionRequest struct {
	AccountID     string
	Slug          string
	Tier          string
	AdminEmail    string
	AdminPassword string
}

// ProvisionResult is the output of a successful provisioning.
type ProvisionResult struct {
	TenantID string
	URL      string
}

// tierResources maps tier names to machine sizing and volume capacity.
type tierResources struct {
	CPUs     int
	MemoryMB int
	VolumGB  int
}

var tiers = map[string]tierResources{
	"free":    {CPUs: 1, MemoryMB: 256, VolumGB: 1},
	"starter": {CPUs: 1, MemoryMB: 512, VolumGB: 1},
	"pro":     {CPUs: 1, MemoryMB: 1024, VolumGB: 5},
}

// Provision creates all infrastructure for a new tenant. On partial failure
// it cleans up resources that were already created (best-effort).
func (p *TenantProvisioner) Provision(req ProvisionRequest) (*ProvisionResult, error) {
	res, ok := tiers[req.Tier]
	if !ok {
		return nil, fmt.Errorf("provision: unknown tier %q", req.Tier)
	}

	tenantID := ulid.Make().String()
	jwtSecret, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("provision: generate jwt secret: %w", err)
	}

	// Step 1: create volume.
	vol, err := p.fly.CreateVolume(fmt.Sprintf("data_%s", req.Slug), res.VolumGB, p.region)
	if err != nil {
		return nil, fmt.Errorf("provision: create volume: %w", err)
	}

	// Step 2: create machine.
	machine, err := p.fly.CreateMachine(fly.MachineConfig{
		Name:     fmt.Sprintf("cogi-%s", req.Slug),
		Image:    p.imageTag,
		CPUs:     res.CPUs,
		MemoryMB: res.MemoryMB,
		VolumeID: vol.ID,
		Env: map[string]string{
			"COGITATOR_WORKSPACE_PATH":   "/data",
			"COGITATOR_SERVER_PORT":      "8484",
			"COGITATOR_JWT_SECRET":       jwtSecret,
			"COGITATOR_INTERNAL_SECRET":  p.secret,
			"COGITATOR_ORCHESTRATOR_URL": "https://app.cogitator.cloud",
			"COGITATOR_TENANT_ID":        tenantID,
		},
		Secrets: map[string]string{
			"COGITATOR_ADMIN_USER":     req.AdminEmail,
			"COGITATOR_ADMIN_PASSWORD": req.AdminPassword,
		},
	}, p.region)
	if err != nil {
		// Cleanup: delete volume.
		_ = p.fly.DeleteVolume(vol.ID)
		return nil, fmt.Errorf("provision: create machine: %w", err)
	}

	// Step 3: add DNS CNAME.
	subdomain := fmt.Sprintf("%s.cogitator.cloud", req.Slug)
	target := fmt.Sprintf("%s.fly.dev", p.flyAppName)
	_, err = p.cf.AddCNAME(subdomain, target, false)
	if err != nil {
		// Cleanup: destroy machine and delete volume.
		_ = p.fly.DestroyMachine(machine.ID)
		_ = p.fly.DeleteVolume(vol.ID)
		return nil, fmt.Errorf("provision: add dns: %w", err)
	}

	// Step 4: store tenant in DB.
	err = p.insertTenant(tenantID, req.AccountID, req.Slug, machine.ID, vol.ID, req.Tier, jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("provision: store tenant: %w", err)
	}

	return &ProvisionResult{
		TenantID: tenantID,
		URL:      fmt.Sprintf("https://%s.cogitator.cloud", req.Slug),
	}, nil
}

// Deprovision tears down all infrastructure for a tenant.
func (p *TenantProvisioner) Deprovision(tenantID string) error {
	machineID, volumeID, slug, err := p.lookupTenant(tenantID)
	if err != nil {
		return fmt.Errorf("deprovision: lookup tenant: %w", err)
	}

	if err := p.fly.StopMachine(machineID); err != nil {
		return fmt.Errorf("deprovision: stop machine: %w", err)
	}

	if err := p.fly.DestroyMachine(machineID); err != nil {
		return fmt.Errorf("deprovision: destroy machine: %w", err)
	}

	if err := p.fly.DeleteVolume(volumeID); err != nil {
		return fmt.Errorf("deprovision: delete volume: %w", err)
	}

	dnsName := fmt.Sprintf("%s.cogitator.cloud", slug)
	rec, err := p.cf.FindRecord(dnsName)
	if err != nil {
		return fmt.Errorf("deprovision: find dns record: %w", err)
	}
	if rec != nil {
		if err := p.cf.DeleteRecord(rec.ID); err != nil {
			return fmt.Errorf("deprovision: delete dns record: %w", err)
		}
	}

	// Clean up associated schedule and metrics data.
	_, _ = p.db.db.Exec(`DELETE FROM wake_schedule WHERE tenant_id = ?`, tenantID)
	_, _ = p.db.db.Exec(`DELETE FROM tenant_heartbeats WHERE tenant_id = ?`, tenantID)
	_, _ = p.db.db.Exec(`DELETE FROM tenant_metrics_baseline WHERE tenant_id = ?`, tenantID)

	if err := p.updateTenantStatus(tenantID, "deleted"); err != nil {
		return fmt.Errorf("deprovision: update status: %w", err)
	}
	return nil
}

// insertTenant stores a new tenant record with status "active".
func (p *TenantProvisioner) insertTenant(id, accountID, slug, machineID, volumeID, tier, jwtSecret string) error {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := p.db.db.Exec(
		`INSERT INTO tenants (id, account_id, slug, fly_machine_id, fly_volume_id, tier, status, jwt_secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, accountID, slug, machineID, volumeID, tier, jwtSecret, now, now,
	)
	return err
}

// lookupTenant retrieves the machine ID, volume ID, and slug for a tenant.
func (p *TenantProvisioner) lookupTenant(tenantID string) (machineID, volumeID, slug string, err error) {
	err = p.db.db.QueryRow(
		`SELECT fly_machine_id, fly_volume_id, slug FROM tenants WHERE id = ?`, tenantID,
	).Scan(&machineID, &volumeID, &slug)
	return
}

// updateTenantStatus sets the status and updated_at for a tenant.
func (p *TenantProvisioner) updateTenantStatus(tenantID, status string) error {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := p.db.db.Exec(
		`UPDATE tenants SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, tenantID,
	)
	return err
}

// randomHex returns a hex-encoded string of n random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

