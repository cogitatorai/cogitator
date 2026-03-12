package orchestrator

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator/cloudflare"
	"github.com/cogitatorai/cogitator/server/internal/orchestrator/fly"
)

// mockFly implements fly.MachineAPI for testing.
type mockFly struct {
	mu              sync.Mutex
	createdVolumes  []fly.Volume
	createdMachines []fly.Machine
	stopped         []string
	destroyed       []string
	deletedVolumes  []string

	failCreateVolume  bool
	failCreateMachine bool
}

func (m *mockFly) CreateVolume(name string, sizeGB int, region string) (*fly.Volume, error) {
	if m.failCreateVolume {
		return nil, errors.New("mock: volume creation failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := fly.Volume{ID: "vol_" + name, Name: name, SizeGB: sizeGB, State: "created"}
	m.createdVolumes = append(m.createdVolumes, v)
	return &v, nil
}

func (m *mockFly) DeleteVolume(volumeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedVolumes = append(m.deletedVolumes, volumeID)
	return nil
}

func (m *mockFly) CreateMachine(cfg fly.MachineConfig, region string) (*fly.Machine, error) {
	if m.failCreateMachine {
		return nil, errors.New("mock: machine creation failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mach := fly.Machine{ID: "mach_" + cfg.Name, Name: cfg.Name, State: "started", Region: region}
	m.createdMachines = append(m.createdMachines, mach)
	return &mach, nil
}

func (m *mockFly) UpdateMachine(machineID string, image string) error { return nil }

func (m *mockFly) StartMachine(machineID string) error { return nil }

func (m *mockFly) StopMachine(machineID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, machineID)
	return nil
}

func (m *mockFly) DestroyMachine(machineID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destroyed = append(m.destroyed, machineID)
	return nil
}

func (m *mockFly) GetMachine(machineID string) (*fly.Machine, error) {
	return &fly.Machine{ID: machineID, State: "started"}, nil
}

// mockCF implements cloudflare.DNSAPI for testing.
type mockCF struct {
	mu      sync.Mutex
	records []cloudflare.DNSRecord
	deleted []string

	failAdd bool
}

func (m *mockCF) AddCNAME(subdomain, target string, proxied bool) (*cloudflare.DNSRecord, error) {
	if m.failAdd {
		return nil, errors.New("mock: dns creation failed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := cloudflare.DNSRecord{ID: "dns_" + subdomain, Type: "CNAME", Name: subdomain, Content: target, Proxied: proxied}
	m.records = append(m.records, r)
	return &r, nil
}

func (m *mockCF) DeleteRecord(recordID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, recordID)
	return nil
}

func (m *mockCF) FindRecord(name string) (*cloudflare.DNSRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, nil
}

func newTestDB(t *testing.T) *OrchestratorDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Insert a dummy account for FK constraints.
	_, err = db.db.Exec(
		`INSERT INTO accounts (id, email, password_hash) VALUES ('acct_1', 'admin@test.com', 'hash')`,
	)
	if err != nil {
		t.Fatalf("insert test account: %v", err)
	}
	return db
}

func newProvisioner(db *OrchestratorDB, f fly.MachineAPI, cf cloudflare.DNSAPI) *TenantProvisioner {
	return NewTenantProvisioner(db, f, cf, "iad", "registry.fly.io/cogitator:latest", "internal-secret", "cogitator-saas")
}

func TestProvision_Success(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	mc := &mockCF{}
	p := newProvisioner(db, mf, mc)

	result, err := p.Provision(ProvisionRequest{
		AccountID:     "acct_1",
		Slug:          "acme",
		Tier:          "starter",
		AdminEmail:    "admin@acme.com",
		AdminPassword: "secret123",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	if result.TenantID == "" {
		t.Fatal("expected non-empty tenant ID")
	}
	if result.URL != "https://acme.cogitator.cloud" {
		t.Fatalf("unexpected URL: %s", result.URL)
	}

	// Volume created.
	if len(mf.createdVolumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(mf.createdVolumes))
	}
	if mf.createdVolumes[0].SizeGB != 1 {
		t.Fatalf("starter tier should have 1GB volume, got %d", mf.createdVolumes[0].SizeGB)
	}

	// Machine created.
	if len(mf.createdMachines) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(mf.createdMachines))
	}

	// DNS record created.
	if len(mc.records) != 1 {
		t.Fatalf("expected 1 DNS record, got %d", len(mc.records))
	}
	if mc.records[0].Proxied {
		t.Fatal("DNS record should not be proxied")
	}

	// Tenant stored in DB.
	var status, tier string
	err = db.db.QueryRow(`SELECT status, tier FROM tenants WHERE id = ?`, result.TenantID).Scan(&status, &tier)
	if err != nil {
		t.Fatalf("query tenant: %v", err)
	}
	if status != "active" {
		t.Fatalf("expected status active, got %s", status)
	}
	if tier != "starter" {
		t.Fatalf("expected tier starter, got %s", tier)
	}
}

func TestProvision_ProTier_VolumeSize(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	mc := &mockCF{}
	p := newProvisioner(db, mf, mc)

	_, err := p.Provision(ProvisionRequest{
		AccountID:     "acct_1",
		Slug:          "bigco",
		Tier:          "pro",
		AdminEmail:    "admin@bigco.com",
		AdminPassword: "secret",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	if mf.createdVolumes[0].SizeGB != 5 {
		t.Fatalf("pro tier should have 5GB volume, got %d", mf.createdVolumes[0].SizeGB)
	}
}

func TestProvision_UnknownTier(t *testing.T) {
	db := newTestDB(t)
	p := newProvisioner(db, &mockFly{}, &mockCF{})

	_, err := p.Provision(ProvisionRequest{
		AccountID: "acct_1",
		Slug:      "test",
		Tier:      "enterprise",
	})
	if err == nil {
		t.Fatal("expected error for unknown tier")
	}
}

func TestProvision_MachineFailure_CleansUpVolume(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{failCreateMachine: true}
	mc := &mockCF{}
	p := newProvisioner(db, mf, mc)

	_, err := p.Provision(ProvisionRequest{
		AccountID:     "acct_1",
		Slug:          "fail",
		Tier:          "free",
		AdminEmail:    "a@b.com",
		AdminPassword: "pass",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Volume should have been created then deleted.
	if len(mf.createdVolumes) != 1 {
		t.Fatalf("expected 1 volume created, got %d", len(mf.createdVolumes))
	}
	if len(mf.deletedVolumes) != 1 {
		t.Fatalf("expected 1 volume deleted, got %d", len(mf.deletedVolumes))
	}
	if mf.deletedVolumes[0] != mf.createdVolumes[0].ID {
		t.Fatal("wrong volume cleaned up")
	}

	// No machine or DNS should exist.
	if len(mf.createdMachines) != 0 {
		t.Fatal("no machine should have been created")
	}
	if len(mc.records) != 0 {
		t.Fatal("no DNS record should have been created")
	}
}

func TestProvision_DNSFailure_CleansUpMachineAndVolume(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	mc := &mockCF{failAdd: true}
	p := newProvisioner(db, mf, mc)

	_, err := p.Provision(ProvisionRequest{
		AccountID:     "acct_1",
		Slug:          "dnsfail",
		Tier:          "free",
		AdminEmail:    "a@b.com",
		AdminPassword: "pass",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Volume and machine should be created then cleaned up.
	if len(mf.createdVolumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(mf.createdVolumes))
	}
	if len(mf.createdMachines) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(mf.createdMachines))
	}
	if len(mf.destroyed) != 1 {
		t.Fatalf("expected 1 machine destroyed, got %d", len(mf.destroyed))
	}
	if mf.destroyed[0] != mf.createdMachines[0].ID {
		t.Fatal("wrong machine destroyed")
	}
	if len(mf.deletedVolumes) != 1 {
		t.Fatalf("expected 1 volume deleted, got %d", len(mf.deletedVolumes))
	}
}

func TestDeprovision_Success(t *testing.T) {
	db := newTestDB(t)
	mf := &mockFly{}
	mc := &mockCF{}
	p := newProvisioner(db, mf, mc)

	// Provision first.
	result, err := p.Provision(ProvisionRequest{
		AccountID:     "acct_1",
		Slug:          "goodbye",
		Tier:          "free",
		AdminEmail:    "a@b.com",
		AdminPassword: "pass",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Reset tracking slices.
	mf.stopped = nil
	mf.destroyed = nil
	mf.deletedVolumes = nil

	err = p.Deprovision(result.TenantID)
	if err != nil {
		t.Fatalf("deprovision: %v", err)
	}

	// Machine stopped and destroyed.
	if len(mf.stopped) != 1 {
		t.Fatalf("expected 1 stop, got %d", len(mf.stopped))
	}
	if len(mf.destroyed) != 1 {
		t.Fatalf("expected 1 destroy, got %d", len(mf.destroyed))
	}

	// Volume deleted.
	if len(mf.deletedVolumes) != 1 {
		t.Fatalf("expected 1 volume deleted, got %d", len(mf.deletedVolumes))
	}

	// DNS record deleted.
	if len(mc.deleted) != 1 {
		t.Fatalf("expected 1 DNS record deleted, got %d", len(mc.deleted))
	}

	// Status updated to deleted.
	var status string
	err = db.db.QueryRow(`SELECT status FROM tenants WHERE id = ?`, result.TenantID).Scan(&status)
	if err != nil {
		t.Fatalf("query tenant: %v", err)
	}
	if status != "deleted" {
		t.Fatalf("expected status deleted, got %s", status)
	}
}
