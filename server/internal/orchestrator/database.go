package orchestrator

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id            TEXT PRIMARY KEY,
	email         TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	is_operator   BOOLEAN NOT NULL DEFAULT 0,
	created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS tenants (
	id             TEXT PRIMARY KEY,
	account_id     TEXT NOT NULL REFERENCES accounts(id),
	slug           TEXT UNIQUE NOT NULL,
	fly_machine_id TEXT NOT NULL DEFAULT '',
	fly_volume_id  TEXT NOT NULL DEFAULT '',
	tier           TEXT NOT NULL DEFAULT 'free',
	status         TEXT NOT NULL DEFAULT 'provisioning',
	jwt_secret     TEXT NOT NULL,
	created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS releases (
	id               TEXT PRIMARY KEY,
	version          TEXT NOT NULL,
	image_tag        TEXT NOT NULL,
	frontend_version TEXT NOT NULL DEFAULT '',
	severity         TEXT NOT NULL DEFAULT 'minor',
	components       TEXT NOT NULL DEFAULT 'all',
	changelog        TEXT NOT NULL DEFAULT '',
	created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS rollouts (
	id                       TEXT PRIMARY KEY,
	release_id               TEXT NOT NULL REFERENCES releases(id),
	status                   TEXT NOT NULL DEFAULT 'pending',
	strategy                 TEXT NOT NULL DEFAULT 'canary',
	previous_image_tag       TEXT NOT NULL DEFAULT '',
	previous_frontend_version TEXT NOT NULL DEFAULT '',
	components               TEXT NOT NULL DEFAULT 'all',
	created_at               DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at               DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS rollout_batches (
	id           TEXT PRIMARY KEY,
	rollout_id   TEXT NOT NULL REFERENCES rollouts(id),
	batch_number INTEGER NOT NULL,
	percentage   INTEGER NOT NULL,
	status       TEXT NOT NULL DEFAULT 'pending',
	started_at   DATETIME,
	completed_at DATETIME
);

CREATE TABLE IF NOT EXISTS rollout_tenants (
	id                TEXT PRIMARY KEY,
	rollout_batch_id  TEXT NOT NULL REFERENCES rollout_batches(id),
	tenant_id         TEXT NOT NULL REFERENCES tenants(id),
	status            TEXT NOT NULL DEFAULT 'pending',
	health_checked_at DATETIME,
	error_message     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS tenant_heartbeats (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	tenant_id      TEXT NOT NULL REFERENCES tenants(id),
	request_count  INTEGER NOT NULL DEFAULT 0,
	error_rate     REAL NOT NULL DEFAULT 0,
	p95_latency_ms REAL NOT NULL DEFAULT 0,
	received_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS tenant_metrics_baseline (
	tenant_id      TEXT PRIMARY KEY REFERENCES tenants(id),
	p95_latency_ms REAL NOT NULL DEFAULT 0,
	error_rate     REAL NOT NULL DEFAULT 0,
	measured_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS wake_schedule (
	tenant_id TEXT PRIMARY KEY REFERENCES tenants(id),
	wake_at   DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
	id                     TEXT PRIMARY KEY,
	tenant_id              TEXT NOT NULL REFERENCES tenants(id),
	stripe_customer_id     TEXT NOT NULL DEFAULT '',
	stripe_subscription_id TEXT NOT NULL DEFAULT '',
	tier                   TEXT NOT NULL DEFAULT 'free',
	status                 TEXT NOT NULL DEFAULT 'active',
	current_period_end     DATETIME,
	created_at             DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_tenants_account_id ON tenants(account_id);
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE INDEX IF NOT EXISTS idx_tenant_heartbeats_tenant_id ON tenant_heartbeats(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_heartbeats_received_at ON tenant_heartbeats(received_at);
CREATE INDEX IF NOT EXISTS idx_wake_schedule_wake_at ON wake_schedule(wake_at);
CREATE INDEX IF NOT EXISTS idx_rollout_tenants_batch_id ON rollout_tenants(rollout_batch_id);
`

// OrchestratorDB wraps the SQLite database for the orchestrator service.
type OrchestratorDB struct {
	db *sql.DB
}

// OpenDB opens (or creates) the orchestrator SQLite database and applies the schema.
func OpenDB(path string) (*OrchestratorDB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, err
	}

	o := &OrchestratorDB{db: sqlDB}

	// Idempotent migration: add is_operator for existing databases that predate this column.
	o.db.Exec(`ALTER TABLE accounts ADD COLUMN is_operator BOOLEAN NOT NULL DEFAULT 0`)

	return o, nil
}

// PromoteOperator sets is_operator = 1 for the account with the given email.
func (o *OrchestratorDB) PromoteOperator(email string) error {
	res, err := o.db.Exec(`UPDATE accounts SET is_operator = 1 WHERE email = ?`, email)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no account found with email %s", email)
	}
	return nil
}

// Close closes the underlying database connection.
func (o *OrchestratorDB) Close() error {
	return o.db.Close()
}
