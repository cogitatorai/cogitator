package database

import (
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestRunNumberedMigrations_AppliesInOrder(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fs := fstest.MapFS{
		"0001_create_foo.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE foo (id INTEGER PRIMARY KEY);"),
		},
		"0002_create_bar.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE bar (id INTEGER PRIMARY KEY);"),
		},
	}

	if err := runNumberedMigrations(db.DB, fs); err != nil {
		t.Fatalf("runNumberedMigrations: %v", err)
	}

	// Verify both tables exist.
	for _, table := range []string{"foo", "bar"} {
		var name string
		if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Errorf("table %q not created: %v", table, err)
		}
	}

	// Verify both versions recorded.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 migration records, got %d", count)
	}
}

func TestRunNumberedMigrations_SkipsApplied(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fs := fstest.MapFS{
		"0001_create_foo.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE foo (id INTEGER PRIMARY KEY);"),
		},
		"0002_create_bar.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE bar (id INTEGER PRIMARY KEY);"),
		},
	}

	// Run once.
	if err := runNumberedMigrations(db.DB, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Run again (should be a no-op, not fail on CREATE TABLE).
	if err := runNumberedMigrations(db.DB, fs); err != nil {
		t.Fatalf("second run: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 migration records after re-run, got %d", count)
	}
}

func TestRunNumberedMigrations_NilFS(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// nil FS should be a no-op.
	if err := runNumberedMigrations(db.DB, nil); err != nil {
		t.Fatalf("runNumberedMigrations with nil FS: %v", err)
	}
}

func TestRunNumberedMigrations_EmptyFS(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fs := fstest.MapFS{}

	if err := runNumberedMigrations(db.DB, fs); err != nil {
		t.Fatalf("runNumberedMigrations with empty FS: %v", err)
	}
}

func TestRunNumberedMigrations_InvalidFilenameSkipped(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fs := fstest.MapFS{
		"0001_valid.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE valid_table (id INTEGER PRIMARY KEY);"),
		},
		"not_a_migration.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE bad (id INTEGER PRIMARY KEY);"),
		},
		"readme.txt": &fstest.MapFile{
			Data: []byte("ignore me"),
		},
	}

	if err := runNumberedMigrations(db.DB, fs); err != nil {
		t.Fatalf("runNumberedMigrations: %v", err)
	}

	// Only the valid migration should have run.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 migration record, got %d", count)
	}

	// The bad table should not exist.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='bad'").Scan(&name)
	if err == nil {
		t.Error("table 'bad' should not have been created")
	}
}

func TestRunNumberedMigrations_RollsBackOnError(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fs := fstest.MapFS{
		"0001_good.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE good (id INTEGER PRIMARY KEY);"),
		},
		"0002_bad.sql": &fstest.MapFile{
			Data: []byte("THIS IS NOT VALID SQL;"),
		},
	}

	err = runNumberedMigrations(db.DB, fs)
	if err == nil {
		t.Fatal("expected error from bad migration")
	}

	// First migration should have succeeded.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count)
	if count != 1 {
		t.Errorf("expected migration 0001 to be recorded, got count %d", count)
	}

	// Second migration should have been rolled back (no version record).
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&count)
	if count != 0 {
		t.Errorf("expected migration 0002 to NOT be recorded, got count %d", count)
	}
}
