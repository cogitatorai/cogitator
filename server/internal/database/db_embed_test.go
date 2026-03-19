package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationCreatesNodeEmbeddings(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify node_embeddings table exists.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='node_embeddings'").Scan(&name)
	if err != nil {
		t.Fatalf("node_embeddings table not found: %v", err)
	}

	// Verify pinned column exists on nodes.
	_, err = db.Exec("SELECT pinned FROM nodes LIMIT 0")
	if err != nil {
		t.Fatalf("pinned column missing: %v", err)
	}

	// Verify consolidated_into column exists on nodes.
	_, err = db.Exec("SELECT consolidated_into FROM nodes LIMIT 0")
	if err != nil {
		t.Fatalf("consolidated_into column missing: %v", err)
	}
}

func TestSystemSettingsAndContentLength(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// system_settings table should exist.
	_, err = db.Exec(`INSERT INTO system_settings (key, value) VALUES ('test_key', 'test_value')`)
	if err != nil {
		t.Fatalf("insert into system_settings: %v", err)
	}
	var val string
	err = db.QueryRow(`SELECT value FROM system_settings WHERE key = 'test_key'`).Scan(&val)
	if err != nil {
		t.Fatalf("select from system_settings: %v", err)
	}
	if val != "test_value" {
		t.Errorf("value = %q, want %q", val, "test_value")
	}

	// content_length column on nodes should exist.
	_, err = db.Exec(`INSERT INTO nodes (id, type, title) VALUES ('test-node', 'fact', 'test')`)
	if err != nil {
		t.Fatal(err)
	}
	var cl sql.NullInt64
	err = db.QueryRow(`SELECT content_length FROM nodes WHERE id = 'test-node'`).Scan(&cl)
	if err != nil {
		t.Fatalf("select content_length: %v", err)
	}
	if cl.Valid {
		t.Error("expected content_length to be NULL for new node")
	}
}
