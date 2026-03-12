package database

import (
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
