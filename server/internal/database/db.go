package database

import (
	"database/sql"
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var EmbeddedMigrations embed.FS

const defaultMaxReaders = 4

type Options struct {
	MaxReaders int
}

type DB struct {
	writer *sql.DB
	reader *sql.DB
}

func (db *DB) Reader() *sql.DB { return db.reader }
func (db *DB) Writer() *sql.DB { return db.writer }

func (db *DB) Close() error {
	rerr := db.reader.Close()
	werr := db.writer.Close()
	return errors.Join(rerr, werr)
}

func Open(path string, opts Options, migrationsFS ...fs.FS) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	basePragmas := "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"

	writer, err := sql.Open("sqlite", path+basePragmas)
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)

	db := &DB{writer: writer}
	if err := db.migrate(); err != nil {
		writer.Close()
		return nil, err
	}

	var mfs fs.FS
	if len(migrationsFS) > 0 {
		mfs = migrationsFS[0]
	}
	if err := runNumberedMigrations(writer, mfs); err != nil {
		writer.Close()
		return nil, err
	}

	reader, err := sql.Open("sqlite", path+basePragmas+"&_pragma=query_only(on)")
	if err != nil {
		writer.Close()
		return nil, err
	}

	maxReaders := opts.MaxReaders
	if maxReaders <= 0 {
		maxReaders = defaultMaxReaders
	}
	reader.SetMaxOpenConns(maxReaders)

	db.reader = reader
	return db, nil
}

func (db *DB) migrate() error {
	if _, err := db.writer.Exec(schema); err != nil {
		return err
	}
	db.writer.Exec(`ALTER TABLE notifications ADD COLUMN sender_id TEXT`)
	db.writer.Exec(`ALTER TABLE tasks ADD COLUMN notify_users TEXT`)
	db.writer.Exec("ALTER TABLE nodes ADD COLUMN content_length INTEGER")
	db.writer.Exec("ALTER TABLE messages ADD COLUMN metadata TEXT")
	db.writer.Exec(`DELETE FROM edges WHERE source_id NOT IN (SELECT id FROM nodes) OR target_id NOT IN (SELECT id FROM nodes)`)
	return nil
}
