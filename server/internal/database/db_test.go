package database

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	tables := []string{"nodes", "edges", "sessions", "messages", "tasks", "task_runs", "pending_jobs", "token_usage"}
	for _, table := range tables {
		var name string
		err := db.Reader().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "dir", "test.db")

	db, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	db.Close()
}

func TestOpenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db1, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	db1.Close()

	db2, err := Open(dbPath, Options{})
	if err != nil {
		t.Fatalf("second Open() error: %v", err)
	}
	db2.Close()
}

func TestInsertAndQueryNode(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Writer().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('test1', 'fact', 'Test Node', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("insert error: %v", err)
	}

	var title string
	err = db.Reader().QueryRow("SELECT title FROM nodes WHERE id = 'test1'").Scan(&title)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if title != "Test Node" {
		t.Errorf("expected 'Test Node', got %q", title)
	}
}

func TestMigrateV7SocialAuth(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var name string
	err = db.Reader().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='user_oauth_links'").Scan(&name)
	if err != nil {
		t.Fatalf("user_oauth_links table not found: %v", err)
	}
}

func TestForeignKeyCascade(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Writer().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('a', 'fact', 'A', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	db.Writer().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('b', 'fact', 'B', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	db.Writer().Exec(`INSERT INTO edges (source_id, target_id, relation) VALUES ('a', 'b', 'supports')`)

	// Deleting node 'a' should cascade and delete the edge
	db.Writer().Exec("DELETE FROM nodes WHERE id = 'a'")

	var count int
	db.Reader().QueryRow("SELECT COUNT(*) FROM edges WHERE source_id = 'a'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 edges after cascade delete, got %d", count)
	}
}

func TestConcurrentReadsWhileWriting(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"), Options{MaxReaders: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Writer().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('seed', 'fact', 'Seed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		t.Fatalf("seed insert error: %v", err)
	}

	tx, err := db.Writer().Begin()
	if err != nil {
		t.Fatalf("begin transaction error: %v", err)
	}

	_, err = tx.Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('txrow', 'fact', 'Tx Row', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err != nil {
		tx.Rollback()
		t.Fatalf("in-transaction insert error: %v", err)
	}

	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var count int
			if scanErr := db.Reader().QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); scanErr != nil {
				errs <- scanErr
			}
		}()
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Errorf("concurrent read failed: %v", e)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit error: %v", err)
	}
}

func TestReaderRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Reader().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('ro', 'fact', 'Read-Only', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err == nil {
		t.Fatal("expected an error when writing via reader pool, got nil")
	}
}

func TestCloseClosesBothPools(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if _, err := db.Reader().Query("SELECT 1"); err == nil {
		t.Error("expected error querying reader after Close(), got nil")
	}

	if _, err := db.Writer().Exec("SELECT 1"); err == nil {
		t.Error("expected error executing on writer after Close(), got nil")
	}
}
