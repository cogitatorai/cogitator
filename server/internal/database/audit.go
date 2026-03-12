package database

import (
	"context"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/security"
)

const auditCreateTable = `
CREATE TABLE IF NOT EXISTS audit_log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	action      TEXT NOT NULL,
	tool        TEXT NOT NULL,
	target      TEXT,
	outcome     TEXT NOT NULL,
	reason      TEXT,
	session_key TEXT,
	task_run_id INTEGER,
	user_id     TEXT,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// auditRequiredColumns lists columns that may be absent in older schemas.
// Each entry is: column name, SQL type with default/constraint.
var auditRequiredColumns = []struct{ name, typedef string }{
	{"action", "TEXT NOT NULL DEFAULT ''"},
	{"tool", "TEXT NOT NULL DEFAULT ''"},
	{"target", "TEXT"},
	{"outcome", "TEXT NOT NULL DEFAULT ''"},
	{"reason", "TEXT"},
	{"session_key", "TEXT"},
	{"task_run_id", "INTEGER"},
	{"user_id", "TEXT"},
	{"created_at", "TIMESTAMP DEFAULT ''"},
}

const auditCreateIndexes = `
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at);
`

// MigrateAudit creates the audit_log table (if absent) and ensures all
// expected columns exist. Pre-existing tables with older schemas are
// upgraded via ALTER TABLE ADD COLUMN.
func (db *DB) MigrateAudit() error {
	if _, err := db.Exec(auditCreateTable); err != nil {
		return err
	}

	// Discover existing columns.
	rows, err := db.Query("PRAGMA table_info(audit_log)")
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	rows.Close()

	// Add any missing columns.
	for _, col := range auditRequiredColumns {
		if existing[col.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE audit_log ADD COLUMN " + col.name + " " + col.typedef); err != nil {
			return err
		}
	}

	_, err = db.Exec(auditCreateIndexes)
	return err
}

// LogAudit inserts an audit event into the audit_log table.
func (db *DB) LogAudit(ctx context.Context, event security.AuditEvent) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO audit_log (action, tool, target, outcome, reason, session_key, task_run_id, user_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Action, event.Tool, event.Target, event.Outcome, event.Reason,
		event.SessionKey, event.TaskRunID, event.UserID,
	)
	return err
}

// AuditLogEntry represents a row from the audit_log table.
type AuditLogEntry struct {
	ID         int64   `json:"id"`
	Action     string  `json:"action"`
	Tool       string  `json:"tool"`
	Target     string  `json:"target,omitempty"`
	Outcome    string  `json:"outcome"`
	Reason     string  `json:"reason,omitempty"`
	SessionKey string  `json:"session_key,omitempty"`
	TaskRunID  *int64  `json:"task_run_id,omitempty"`
	UserID     *string `json:"user_id,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// AuditQuery specifies filters and pagination for listing audit logs.
type AuditQuery struct {
	Limit   int
	Offset  int
	Action  string // filter by action type
	Outcome string // filter by "allowed" or "blocked"
	UserID  string // scope to a specific user (empty = all)
}

// ListAuditLogs returns audit log entries matching the query filters.
// Returns entries and total count (for pagination).
func (db *DB) ListAuditLogs(q AuditQuery) ([]AuditLogEntry, int, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}

	where := []string{"1=1"}
	args := []any{}

	if q.Action != "" {
		where = append(where, "action = ?")
		args = append(args, q.Action)
	}
	if q.Outcome != "" {
		where = append(where, "outcome = ?")
		args = append(args, q.Outcome)
	}
	if q.UserID != "" {
		where = append(where, "user_id = ?")
		args = append(args, q.UserID)
	}

	clause := "WHERE " + strings.Join(where, " AND ")

	var total int
	countSQL := "SELECT COUNT(*) FROM audit_log " + clause
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	querySQL := "SELECT id, action, tool, COALESCE(target,''), outcome, COALESCE(reason,''), COALESCE(session_key,''), task_run_id, user_id, created_at FROM audit_log " +
		clause + " ORDER BY id DESC LIMIT ? OFFSET ?"
	queryArgs := append(args, q.Limit, q.Offset)

	rows, err := db.Query(querySQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.Tool, &e.Target, &e.Outcome, &e.Reason, &e.SessionKey, &e.TaskRunID, &e.UserID, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []AuditLogEntry{}
	}
	return entries, total, rows.Err()
}
