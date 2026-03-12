package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// runNumberedMigrations applies ordered SQL migration files from the given
// filesystem exactly once. Each file must follow the naming convention
// NNNN_description.sql (e.g. 0001_add_foo.sql). The version number is
// extracted from the filename prefix and tracked in the schema_migrations
// table to prevent re-application.
//
// If migrationsFS is nil or contains no .sql files, this is a no-op.
// Each migration runs inside its own transaction: the SQL is executed and
// the version is recorded atomically, so a failed migration never leaves
// a partial record.
func runNumberedMigrations(db *sql.DB, migrationsFS fs.FS) error {
	if migrationsFS == nil {
		return nil
	}

	files, err := collectMigrationFiles(migrationsFS)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, mf := range files {
		if applied[mf.version] {
			continue
		}
		if err := applyMigration(db, migrationsFS, mf); err != nil {
			return fmt.Errorf("migration %s: %w", mf.name, err)
		}
	}
	return nil
}

type migrationFile struct {
	name    string
	version int
}

// collectMigrationFiles reads .sql files from the FS root, parses version
// numbers from filenames, and returns them sorted by version ascending.
// Files that do not match the NNNN_*.sql pattern are silently skipped.
func collectMigrationFiles(fsys fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migration dir: %w", err)
	}

	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".sql" {
			continue
		}
		version, ok := parseVersion(name)
		if !ok {
			continue
		}
		files = append(files, migrationFile{name: name, version: version})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})
	return files, nil
}

// parseVersion extracts the leading integer from a filename like "0001_foo.sql".
// Returns (1, true) for that example. Returns (0, false) if the name does not
// start with digits followed by an underscore.
func parseVersion(name string) (int, bool) {
	idx := strings.IndexByte(name, '_')
	if idx <= 0 {
		return 0, false
	}
	v, err := strconv.Atoi(name[:idx])
	if err != nil {
		return 0, false
	}
	return v, true
}

// appliedVersions returns the set of migration versions already recorded
// in the schema_migrations table.
func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// applyMigration runs a single migration file inside a transaction: it
// executes the SQL and records the version atomically.
func applyMigration(db *sql.DB, fsys fs.FS, mf migrationFile) error {
	data, err := fs.ReadFile(fsys, mf.name)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(string(data)); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", mf.version); err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	return tx.Commit()
}
