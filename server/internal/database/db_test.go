package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	tables := []string{"nodes", "edges", "sessions", "messages", "tasks", "task_runs", "pending_jobs", "token_usage"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "dir", "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	db.Close()
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	db1.Close()

	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open() error: %v", err)
	}
	db2.Close()
}

func TestInsertAndQueryNode(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('test1', 'fact', 'Test Node', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert error: %v", err)
	}

	var title string
	err = db.QueryRow("SELECT title FROM nodes WHERE id = 'test1'").Scan(&title)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if title != "Test Node" {
		t.Errorf("expected 'Test Node', got %q", title)
	}
}

func TestMigrateV7SocialAuth(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='user_oauth_links'").Scan(&name)
	if err != nil {
		t.Fatalf("user_oauth_links table not found: %v", err)
	}
}

func TestForeignKeyCascade(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('a', 'fact', 'A', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	db.Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('b', 'fact', 'B', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	db.Exec(`INSERT INTO edges (source_id, target_id, relation) VALUES ('a', 'b', 'supports')`)

	// Deleting node 'a' should cascade and delete the edge
	db.Exec("DELETE FROM nodes WHERE id = 'a'")

	var count int
	db.QueryRow("SELECT COUNT(*) FROM edges WHERE source_id = 'a'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 edges after cascade delete, got %d", count)
	}
}
