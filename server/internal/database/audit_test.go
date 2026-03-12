package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/security"
)

func TestMigrateAudit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.MigrateAudit(); err != nil {
		t.Fatalf("MigrateAudit() error: %v", err)
	}

	// Verify the table exists.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='audit_log'").Scan(&name)
	if err != nil {
		t.Fatalf("audit_log table not found: %v", err)
	}
}

func TestMigrateAuditUpgradesLegacySchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate a legacy table that only has id, created_at, and a text column.
	_, err = db.Exec(`CREATE TABLE audit_log (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}

	// MigrateAudit must succeed, adding the missing columns.
	if err := db.MigrateAudit(); err != nil {
		t.Fatalf("MigrateAudit() on legacy schema: %v", err)
	}

	// Verify we can insert a full row.
	ctx := context.Background()
	if err := db.LogAudit(ctx, security.AuditEvent{
		Action:  "shell_exec",
		Tool:    "shell",
		Target:  "ls",
		Outcome: "allowed",
	}); err != nil {
		t.Fatalf("LogAudit() after upgrade: %v", err)
	}
}

func TestMigrateAuditIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.MigrateAudit(); err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateAudit(); err != nil {
		t.Fatalf("second MigrateAudit() error: %v", err)
	}
}

func TestLogAuditInsertAndQuery(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.MigrateAudit(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	taskRunID := int64(42)

	events := []security.AuditEvent{
		{
			Action:     "shell_exec",
			Tool:       "shell",
			Target:     "ls -la",
			Outcome:    "allowed",
			SessionKey: "sess-1",
		},
		{
			Action:     "tool_blocked",
			Tool:       "shell",
			Target:     "cat ~/.ssh/id_rsa",
			Outcome:    "blocked",
			Reason:     "references sensitive path: ~/.ssh",
			SessionKey: "sess-1",
			TaskRunID:  &taskRunID,
		},
	}

	for _, ev := range events {
		if err := db.LogAudit(ctx, ev); err != nil {
			t.Fatalf("LogAudit() error: %v", err)
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 audit rows, got %d", count)
	}

	// Verify a blocked event was stored correctly.
	var action, outcome, reason string
	err = db.QueryRow(
		"SELECT action, outcome, reason FROM audit_log WHERE outcome = 'blocked'",
	).Scan(&action, &outcome, &reason)
	if err != nil {
		t.Fatal(err)
	}
	if action != "tool_blocked" {
		t.Errorf("action = %q, want tool_blocked", action)
	}
	if reason != "references sensitive path: ~/.ssh" {
		t.Errorf("reason = %q, want 'references sensitive path: ~/.ssh'", reason)
	}
}

func setupAuditDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MigrateAudit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedAuditLogs(t *testing.T, db *DB, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		outcome := "allowed"
		action := "shell_exec"
		if i%3 == 0 {
			outcome = "blocked"
			action = "tool_blocked"
		}
		if err := db.LogAudit(ctx, security.AuditEvent{
			Action:  action,
			Tool:    "shell",
			Target:  "cmd-" + string(rune('a'+i%26)),
			Outcome: outcome,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListAuditLogsPagination(t *testing.T) {
	db := setupAuditDB(t)
	seedAuditLogs(t, db, 10)

	entries, total, err := db.ListAuditLogs(AuditQuery{Limit: 3, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 {
		t.Errorf("total = %d, want 10", total)
	}
	if len(entries) != 3 {
		t.Errorf("len(entries) = %d, want 3", len(entries))
	}

	// Second page.
	entries2, total2, err := db.ListAuditLogs(AuditQuery{Limit: 3, Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 10 {
		t.Errorf("total = %d, want 10", total2)
	}
	if len(entries2) != 3 {
		t.Errorf("len(entries) = %d, want 3", len(entries2))
	}
	// Entries should be different (ordered by id DESC).
	if entries[0].ID == entries2[0].ID {
		t.Error("pages should return different entries")
	}
}

func TestListAuditLogsFilterByAction(t *testing.T) {
	db := setupAuditDB(t)
	seedAuditLogs(t, db, 9)

	entries, total, err := db.ListAuditLogs(AuditQuery{Action: "tool_blocked"})
	if err != nil {
		t.Fatal(err)
	}
	// Indices 0,3,6 are blocked (3 out of 9).
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	for _, e := range entries {
		if e.Action != "tool_blocked" {
			t.Errorf("action = %q, want tool_blocked", e.Action)
		}
	}
}

func TestListAuditLogsFilterByOutcome(t *testing.T) {
	db := setupAuditDB(t)
	seedAuditLogs(t, db, 9)

	entries, total, err := db.ListAuditLogs(AuditQuery{Outcome: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	for _, e := range entries {
		if e.Outcome != "blocked" {
			t.Errorf("outcome = %q, want blocked", e.Outcome)
		}
	}
}

func TestListAuditLogsEmpty(t *testing.T) {
	db := setupAuditDB(t)

	entries, total, err := db.ListAuditLogs(AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

func TestListAuditLogsDefaultAndMaxLimit(t *testing.T) {
	db := setupAuditDB(t)
	seedAuditLogs(t, db, 5)

	// Default limit (0 should become 50, but we only have 5 entries).
	entries, _, err := db.ListAuditLogs(AuditQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries with default limit, got %d", len(entries))
	}

	// Max limit clamping: requesting 999 should be capped to 200.
	entries, _, err = db.ListAuditLogs(AuditQuery{Limit: 999})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}
