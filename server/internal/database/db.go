package database

import (
	"database/sql"
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var EmbeddedMigrations embed.FS

type DB struct {
	*sql.DB
}

// Open creates or opens a SQLite database at path, applying the idempotent
// schema and any numbered migrations. An optional fs.FS containing numbered
// migration SQL files can be provided; pass nil when no migrations exist.
func Open(path string, migrationsFS ...fs.FS) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	var mfs fs.FS
	if len(migrationsFS) > 0 {
		mfs = migrationsFS[0]
	}
	if err := runNumberedMigrations(sqlDB, mfs); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) migrate() error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	// Idempotent column additions for existing databases.
	db.Exec(`ALTER TABLE notifications ADD COLUMN sender_id TEXT`)
	db.Exec(`ALTER TABLE tasks ADD COLUMN notify_users TEXT`)
	db.Exec("ALTER TABLE nodes ADD COLUMN content_length INTEGER")
	// Remove orphaned edges whose source or target no longer exists.
	db.Exec(`DELETE FROM edges WHERE source_id NOT IN (SELECT id FROM nodes) OR target_id NOT IN (SELECT id FROM nodes)`)
	return nil
}
