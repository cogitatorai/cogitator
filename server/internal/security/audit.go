package security

import "context"

// AuditEvent records a security-relevant action performed by a tool.
type AuditEvent struct {
	Action     string // "shell_exec", "file_read", "file_write", "tool_blocked", "skill_install"
	Tool       string // tool name
	Target     string // command, file path, or skill slug
	Outcome    string // "allowed", "blocked"
	Reason     string // why blocked (empty if allowed)
	SessionKey string
	TaskRunID  *int64
	UserID     *string
}

// AuditLogger persists security audit events.
type AuditLogger interface {
	LogAudit(ctx context.Context, event AuditEvent) error
}
