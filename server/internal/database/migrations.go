package database

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id                TEXT PRIMARY KEY,
	email             TEXT UNIQUE NOT NULL,
	name              TEXT NOT NULL DEFAULT '',
	password_hash     TEXT NOT NULL,
	role              TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin','moderator','user')),
	profile_overrides TEXT NOT NULL DEFAULT '{}',
	created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS invite_codes (
	code       TEXT PRIMARY KEY,
	created_by TEXT NOT NULL REFERENCES users(id),
	role       TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin','moderator','user')),
	redeemed_by TEXT REFERENCES users(id),
	expires_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at DATETIME NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS user_oauth_links (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider   TEXT NOT NULL,
	subject    TEXT NOT NULL,
	email      TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	UNIQUE(provider, subject)
);

CREATE TABLE IF NOT EXISTS nodes (
	id                 TEXT PRIMARY KEY,
	type               TEXT NOT NULL,
	title              TEXT NOT NULL,
	summary            TEXT,
	tags               TEXT,
	retrieval_triggers TEXT,
	confidence         REAL DEFAULT 0.5,
	content_path       TEXT,
	enrichment_status  TEXT DEFAULT 'pending',
	origin             TEXT,
	source_url         TEXT,
	version            TEXT,
	skill_path         TEXT,
	pinned             BOOLEAN NOT NULL DEFAULT 0,
	private            BOOLEAN NOT NULL DEFAULT 0,
	consolidated_into  TEXT DEFAULT '',
	user_id            TEXT REFERENCES users(id),
	subject_id         TEXT,
	created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_accessed      TIMESTAMP
);

CREATE TABLE IF NOT EXISTS node_embeddings (
	node_id    TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
	embedding  BLOB NOT NULL,
	model      TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS edges (
	source_id  TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
	target_id  TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
	relation   TEXT NOT NULL,
	weight     REAL DEFAULT 0.5,
	user_id    TEXT REFERENCES users(id),
	private    BOOLEAN NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (source_id, target_id, relation)
);

CREATE TABLE IF NOT EXISTS sessions (
	key         TEXT PRIMARY KEY,
	channel     TEXT NOT NULL,
	chat_id     TEXT NOT NULL,
	summary     TEXT,
	is_active   BOOLEAN DEFAULT 0,
	private     BOOLEAN NOT NULL DEFAULT 0,
	user_id     TEXT REFERENCES users(id),
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_active TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	session_key  TEXT NOT NULL REFERENCES sessions(key) ON DELETE CASCADE,
	role         TEXT NOT NULL,
	content      TEXT,
	tool_calls   TEXT,
	tool_call_id TEXT,
	tools_used   TEXT,
	user_id      TEXT REFERENCES users(id),
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tasks (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL,
	prompt        TEXT NOT NULL,
	cron_expr     TEXT,
	model_tier    TEXT DEFAULT 'cheap',
	enabled       BOOLEAN DEFAULT 1,
	max_retries   INTEGER DEFAULT 3,
	retry_backoff INTEGER DEFAULT 60,
	timeout       INTEGER DEFAULT 300,
	working_dir   TEXT,
	notify        TEXT,
	allow_manual  BOOLEAN DEFAULT 1,
	notify_chat   BOOLEAN DEFAULT 0,
	broadcast     BOOLEAN DEFAULT 0,
	notify_users  TEXT,
	user_id       TEXT REFERENCES users(id),
	created_by    TEXT,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task_runs (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id         INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
	trigger         TEXT NOT NULL,
	started_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	finished_at     TIMESTAMP,
	status          TEXT NOT NULL DEFAULT 'running',
	model_used      TEXT,
	error_message   TEXT,
	error_class     TEXT,
	result_summary  TEXT,
	transcript_path TEXT,
	skills_used     TEXT,
	fix_applied     TEXT,
	session_key     TEXT,
	parent_run_id   INTEGER REFERENCES task_runs(id),
	retry_of        INTEGER REFERENCES task_runs(id),
	tool_calls      TEXT,
	user_id         TEXT REFERENCES users(id),
	created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pending_jobs (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	job_type   TEXT NOT NULL,
	payload    TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'pending',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS token_usage (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	model_tier  TEXT NOT NULL,
	model_name  TEXT NOT NULL,
	tokens_in   INTEGER NOT NULL DEFAULT 0,
	tokens_out  INTEGER NOT NULL DEFAULT 0,
	task_run_id INTEGER,
	session_key TEXT,
	user_id     TEXT REFERENCES users(id),
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notifications (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      TEXT,
	sender_id    TEXT,
	task_id      INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
	task_name    TEXT NOT NULL,
	run_id       INTEGER,
	trigger_type TEXT NOT NULL,
	status       TEXT NOT NULL,
	content      TEXT NOT NULL,
	read         BOOLEAN NOT NULL DEFAULT 0,
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS push_tokens (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    TEXT NOT NULL,
	token      TEXT NOT NULL UNIQUE,
	platform   TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_enrichment ON nodes(enrichment_status);
CREATE INDEX IF NOT EXISTS idx_nodes_user ON nodes(user_id);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_user ON edges(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_key);
CREATE INDEX IF NOT EXISTS idx_messages_user ON messages(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_task ON task_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_pending_jobs_status ON pending_jobs(status);
CREATE INDEX IF NOT EXISTS idx_token_usage_date ON token_usage(created_at);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(user_id, read);
CREATE INDEX IF NOT EXISTS idx_push_tokens_user ON push_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_invite_codes_redeemed ON invite_codes(redeemed_by);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_links_user ON user_oauth_links(user_id);

CREATE TABLE IF NOT EXISTS system_settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS subscription_status (
	id            INTEGER PRIMARY KEY CHECK (id = 1),
	status        TEXT NOT NULL DEFAULT 'active',
	grace_ends_at TEXT,
	updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);
`
