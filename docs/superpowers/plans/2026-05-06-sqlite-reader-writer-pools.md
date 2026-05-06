# SQLite Reader/Writer Pool Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the single `*sql.DB` connection into writer (1 conn) and reader (N conns) pools so concurrent reads are no longer blocked by writes.

**Architecture:** The `database.DB` struct drops its embedded `*sql.DB` and exposes `Reader()` and `Writer()` accessors backed by separate pools. The reader pool opens with `query_only` pragma. All callers migrate to explicit pool selection. A new `database.max_readers` config field (default 4) controls reader pool size.

**Tech Stack:** Go, SQLite (WAL mode), modernc.org/sqlite driver

---

### Task 1: Add DatabaseConfig to config and thread through app.New()

**Files:**
- Modify: `server/internal/config/config.go:22-36` (Config struct)
- Modify: `server/internal/config/config.go:160-208` (Default func)
- Modify: `server/internal/config/config.go:243-337` (ApplyEnv func)
- Modify: `server/internal/app/server.go:138-142` (database.Open call)

- [ ] **Step 1: Add DatabaseConfig struct and field to Config**

In `server/internal/config/config.go`, add the struct definition before the `Config` struct:

```go
type DatabaseConfig struct {
	MaxReaders int `yaml:"max_readers" json:"max_readers"`
}
```

Add the field to `Config`:

```go
type Config struct {
	Server       ServerConfig              `yaml:"server"`
	Models       ModelsConfig              `yaml:"models"`
	Providers    map[string]ProviderConfig `yaml:"providers"`
	Resources    ResourcesConfig           `yaml:"resources"`
	Memory       MemoryConfig              `yaml:"memory"`
	Reflection   ReflectionConfig          `yaml:"reflection"`
	Tasks        TasksConfig               `yaml:"tasks"`
	Workspace    WorkspaceConfig           `yaml:"workspace"`
	Channels     ChannelsConfig            `yaml:"channels"`
	Security     SecurityConfig            `yaml:"security"`
	Update       UpdateConfig              `yaml:"update"`
	Voice        VoiceConfig               `yaml:"voice"`
	Optimization OptimizationConfig        `yaml:"optimization"`
	Database     DatabaseConfig            `yaml:"database"`
}
```

- [ ] **Step 2: Set default in Default()**

In the `Default()` function, add:

```go
Database: DatabaseConfig{
	MaxReaders: 4,
},
```

- [ ] **Step 3: Add env var override in ApplyEnv()**

At the end of `ApplyEnv()`:

```go
if v := os.Getenv("COGITATOR_DATABASE_MAX_READERS"); v != "" {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		c.Database.MaxReaders = n
	}
}
```

- [ ] **Step 4: Verify config builds**

Run: `cd server && go build ./internal/config/`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add server/internal/config/config.go
git commit -m "config: add database.max_readers setting"
```

---

### Task 2: Rewrite database.DB struct with reader/writer pools

**Files:**
- Modify: `server/internal/database/db.go` (full rewrite)

- [ ] **Step 1: Write failing test for dual-pool Open**

In `server/internal/database/db_test.go`, update `TestOpenAndMigrate` to use the new signature:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/database/ -run TestOpenAndMigrate -v`
Expected: FAIL (compilation error, Open signature doesn't match)

- [ ] **Step 3: Rewrite db.go with dual pools**

Replace the contents of `server/internal/database/db.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/database/ -run TestOpenAndMigrate -v`
Expected: PASS

- [ ] **Step 5: Update remaining tests in db_test.go**

Update all other tests that call `Open` to use the new signature (`Open(path, Options{})`) and use `db.Writer()` / `db.Reader()` instead of the embedded `*sql.DB`:

```go
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

	db.Writer().Exec("DELETE FROM nodes WHERE id = 'a'")

	var count int
	db.Reader().QueryRow("SELECT COUNT(*) FROM edges WHERE source_id = 'a'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 edges after cascade delete, got %d", count)
	}
}
```

- [ ] **Step 6: Update audit_test.go, db_embed_test.go, and migration_runner_test.go**

Update all `Open()` calls in these files to use the new `Open(path, Options{}, ...)` signature. Replace any direct `db.Exec/db.Query/db.QueryRow` with `db.Writer().Exec/db.Reader().Query/db.Reader().QueryRow` as appropriate (writes through Writer, reads through Reader).

- [ ] **Step 7: Run all database package tests**

Run: `cd server && go test ./internal/database/ -v`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add server/internal/database/db.go server/internal/database/db_test.go \
  server/internal/database/audit_test.go server/internal/database/db_embed_test.go \
  server/internal/database/migration_runner_test.go
git commit -m "database: split into reader/writer pools with explicit accessors"
```

---

### Task 3: Add pool-specific tests (concurrency, read-only enforcement, close)

**Files:**
- Modify: `server/internal/database/db_test.go`

- [ ] **Step 1: Write concurrency test**

```go
func TestConcurrentReadsWhileWriting(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{MaxReaders: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Writer().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('n1', 'fact', 'Node', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	tx, err := db.Writer().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	tx.Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('n2', 'fact', 'Node2', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var count int
			if err := db.Reader().QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent read failed: %v", err)
	}

	tx.Commit()
}
```

Add `"sync"` to the test file imports.

- [ ] **Step 2: Run test to verify it passes**

Run: `cd server && go test ./internal/database/ -run TestConcurrentReadsWhileWriting -v`
Expected: PASS

- [ ] **Step 3: Write read-only enforcement test**

```go
func TestReaderRejectsWrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Reader().Exec(`INSERT INTO nodes (id, type, title, created_at, updated_at)
		VALUES ('bad', 'fact', 'Should Fail', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if err == nil {
		t.Fatal("expected error when writing through reader pool, got nil")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/database/ -run TestReaderRejectsWrites -v`
Expected: PASS

- [ ] **Step 5: Write close test**

```go
func TestCloseCloseBothPools(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	_, err = db.Reader().Query("SELECT 1")
	if err == nil {
		t.Error("expected error from Reader after Close")
	}

	_, err = db.Writer().Exec("SELECT 1")
	if err == nil {
		t.Error("expected error from Writer after Close")
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd server && go test ./internal/database/ -run TestCloseCloseBothPools -v`
Expected: PASS

- [ ] **Step 7: Run all database tests**

Run: `cd server && go test ./internal/database/ -v`
Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add server/internal/database/db_test.go
git commit -m "database: add concurrency, read-only enforcement, and close tests"
```

---

### Task 4: Migrate database package methods (audit.go, usage.go)

**Files:**
- Modify: `server/internal/database/audit.go:46-83` (MigrateAudit: writes), `audit.go:86-94` (LogAudit: write), `audit.go:121-175` (ListAuditLogs: reads)
- Modify: `server/internal/database/usage.go:14-21` (RecordTokenUsage: write), `usage.go:25-37` (TodayTokenUsage: read), `usage.go:41-77` (DailyTokenStats: read)

- [ ] **Step 1: Migrate audit.go**

`MigrateAudit`: All operations are DDL/DML (CREATE TABLE, ALTER TABLE, CREATE INDEX). Use `db.writer`:

```go
func (db *DB) MigrateAudit() error {
	if _, err := db.writer.Exec(auditCreateTable); err != nil {
		return err
	}

	rows, err := db.writer.Query("PRAGMA table_info(audit_log)")
	if err != nil {
		return err
	}
	// ... rest stays the same but replace db.Exec with db.writer.Exec ...
```

Replace every `db.Exec` in `MigrateAudit` with `db.writer.Exec` and `db.Query` with `db.writer.Query` (the PRAGMA query is part of the migration flow).

`LogAudit` (line 86): Replace `db.ExecContext` with `db.writer.ExecContext`.

`ListAuditLogs` (line 121): Replace `db.QueryRow` (line 149) with `db.reader.QueryRow`, and `db.Query` (line 157) with `db.reader.Query`.

- [ ] **Step 2: Migrate usage.go**

`RecordTokenUsage` (line 14): Replace `db.Exec` with `db.writer.Exec`.

`TodayTokenUsage` (line 25): Replace `db.QueryRow` with `db.reader.QueryRow`.

`DailyTokenStats` (line 41): Replace `db.Query` with `db.reader.Query`.

- [ ] **Step 3: Run database tests**

Run: `cd server && go test ./internal/database/ -v`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/internal/database/audit.go server/internal/database/usage.go
git commit -m "database: migrate audit and usage methods to explicit pools"
```

---

### Task 5: Migrate memory store

**Files:**
- Modify: `server/internal/memory/store.go`

This is the largest store. Every `s.db.Query`, `s.db.QueryRow` becomes `s.db.Reader().Query`, `s.db.Reader().QueryRow`. Every `s.db.Exec` becomes `s.db.Writer().Exec`. The one `s.db.Begin()` (line 119) becomes `s.db.Writer().Begin()`.

- [ ] **Step 1: Migrate all read operations**

Replace in `server/internal/memory/store.go`:

| Line(s) | Current | New |
|---------|---------|-----|
| 74 | `s.db.QueryRow(` | `s.db.Reader().QueryRow(` |
| 154 | `s.db.QueryRow(` | `s.db.Reader().QueryRow(` |
| 208 | `s.db.Query(` | `s.db.Reader().Query(` |
| 243 | `s.db.Query(` | `s.db.Reader().Query(` |
| 278 | `s.db.Query(` | `s.db.Reader().Query(` |
| 306 | `s.db.QueryRow(` | `s.db.Reader().QueryRow(` |
| 359 | `s.db.Query(` | `s.db.Reader().Query(` |
| 395 | `s.db.Query(` | `s.db.Reader().Query(` |
| 443 | `s.db.QueryRow(` (x3) | `s.db.Reader().QueryRow(` |
| 456 | `s.db.Query(` | `s.db.Reader().Query(` |
| 502 | `s.db.QueryRow(` | `s.db.Reader().QueryRow(` |
| 523 | `s.db.Query(` | `s.db.Reader().Query(` |
| 583 | `s.db.Query(` | `s.db.Reader().Query(` |
| 641 | `s.db.Query(` | `s.db.Reader().Query(` |
| 651 | `s.db.Query(` | `s.db.Reader().Query(` |
| 668 | `s.db.QueryRow(` | `s.db.Reader().QueryRow(` |
| 675 | `s.db.Query(` | `s.db.Reader().Query(` |
| 704 | `s.db.Query(` | `s.db.Reader().Query(` |
| 748 | `s.db.Query(` | `s.db.Reader().Query(` |

- [ ] **Step 2: Migrate all write operations**

| Line(s) | Current | New |
|---------|---------|-----|
| 53 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 119 | `s.db.Begin()` | `s.db.Writer().Begin()` |
| 169 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 181 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 312 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 429 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 435 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 494 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 543 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 550 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 615 | `s.db.Exec(` | `s.db.Writer().Exec(` |
| 761-767 | `s.db.Exec(` (x2) | `s.db.Writer().Exec(` |

- [ ] **Step 3: Run memory package tests**

Run: `cd server && go test ./internal/memory/ -v -count=1`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/internal/memory/store.go
git commit -m "memory: migrate store to explicit reader/writer pools"
```

---

### Task 6: Migrate session store

**Files:**
- Modify: `server/internal/session/store.go`

- [ ] **Step 1: Migrate all operations**

Reads (QueryRow/Query):
- Line 73: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 109: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 112: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 120: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 128: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 131: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 195: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 220: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 273: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 276: `s.db.Query(` -> `s.db.Reader().Query(`

Writes (Exec):
- Line 82: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 101: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 152: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 155: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 169: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 176: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 225: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 232: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 236: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 244: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 256: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 258: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 263: `s.db.Exec(` -> `s.db.Writer().Exec(`

- [ ] **Step 2: Run session package tests**

Run: `cd server && go test ./internal/session/ -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/internal/session/store.go
git commit -m "session: migrate store to explicit reader/writer pools"
```

---

### Task 7: Migrate task store

**Files:**
- Modify: `server/internal/task/store.go`

- [ ] **Step 1: Migrate all operations**

Reads (QueryRow/Query):
- Line 79: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 168: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 201: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 265: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 305: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 440: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 456: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 493: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 546: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 559: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 611: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 639: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`

Writes (Exec):
- Line 55: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 129: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 142: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 145: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 240: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 253: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 280: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 347: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 355: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 362: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 368: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 374: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 399: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 416: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 425: `s.db.Exec(` -> `s.db.Writer().Exec(`

- [ ] **Step 2: Run task package tests**

Run: `cd server && go test ./internal/task/ -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/internal/task/store.go
git commit -m "task: migrate store to explicit reader/writer pools"
```

---

### Task 8: Migrate user store

**Files:**
- Modify: `server/internal/user/store.go`

- [ ] **Step 1: Migrate all operations**

Reads (QueryRow/Query):
- Line 74: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 90: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 105: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 128: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 239: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 312: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 405: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 423: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 467: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 271: `s.db.Query(` -> `s.db.Reader().Query(`

Writes (Exec):
- Line 56: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 134: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 143: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 156: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 161: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 175: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 190: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 223: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 257: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 293: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 299: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 329: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 335: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 342: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 373: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 389: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 447: `s.db.Exec(` -> `s.db.Writer().Exec(`

- [ ] **Step 2: Run user package tests**

Run: `cd server && go test ./internal/user/ -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/internal/user/store.go
git commit -m "user: migrate store to explicit reader/writer pools"
```

---

### Task 9: Migrate notification and push stores

**Files:**
- Modify: `server/internal/notification/store.go`
- Modify: `server/internal/push/store.go`

- [ ] **Step 1: Migrate notification store**

Reads:
- Line 63: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`
- Line 69: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 92: `s.db.QueryRow(` -> `s.db.Reader().QueryRow(`

Writes:
- Line 45: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 99: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 105: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 114: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 121: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 127: `s.db.Exec(` -> `s.db.Writer().Exec(`

- [ ] **Step 2: Migrate push store**

Reads:
- Line 35: `s.db.Query(` -> `s.db.Reader().Query(`
- Line 57: `s.db.Query(` -> `s.db.Reader().Query(`

Writes:
- Line 26: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 78: `s.db.Exec(` -> `s.db.Writer().Exec(`
- Line 84: `s.db.Exec(` -> `s.db.Writer().Exec(`

- [ ] **Step 3: Run notification and push tests**

Run: `cd server && go test ./internal/notification/ ./internal/push/ -v -count=1`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/internal/notification/store.go server/internal/push/store.go
git commit -m "notification, push: migrate stores to explicit reader/writer pools"
```

---

### Task 10: Migrate API router direct DB access

**Files:**
- Modify: `server/internal/api/subscription.go:20,56,78` (read/write)
- Modify: `server/internal/api/user_handlers.go:184,358` (write/read)

- [ ] **Step 1: Migrate subscription.go**

- Line 20: `rt.db.QueryRow(` -> `rt.db.Reader().QueryRow(` (read subscription status)
- Line 56: `rt.db.Exec(` -> `rt.db.Writer().Exec(` (upsert subscription status)
- Line 78: `rt.db.QueryRow(` -> `rt.db.Reader().QueryRow(` (read subscription status)

- [ ] **Step 2: Migrate user_handlers.go**

- Line 184: `r.db.Exec(q, targetID)` -> `r.db.Writer().Exec(q, targetID)` (cleanup queries on user deletion)
- Line 358: `r.db.QueryRow(` -> `r.db.Reader().QueryRow(` (count admins)

- [ ] **Step 3: Run API tests**

Run: `cd server && go test ./internal/api/ -v -count=1`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/internal/api/subscription.go server/internal/api/user_handlers.go
git commit -m "api: migrate direct DB access to explicit reader/writer pools"
```

---

### Task 11: Migrate worker/backfill.go

**Files:**
- Modify: `server/internal/worker/backfill.go:52,82,108`

- [ ] **Step 1: Migrate backfill operations**

- Line 52: `store.DB().Query(` -> `store.DB().Reader().Query(` (select nodes for re-enrichment)
- Line 82: `store.DB().Exec(` -> `store.DB().Writer().Exec(` (update enrichment status)
- Line 108: `store.DB().Query(` -> `store.DB().Reader().Query(` (select nodes for content-length backfill)

- [ ] **Step 2: Run worker tests**

Run: `cd server && go test ./internal/worker/ -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/internal/worker/backfill.go
git commit -m "worker: migrate backfill DB access to explicit reader/writer pools"
```

---

### Task 12: Update app.New() call site and thread config

**Files:**
- Modify: `server/internal/app/server.go:138-142`

- [ ] **Step 1: Update database.Open call**

Change line 139 from:

```go
db, err := database.Open(ws.DBPath(), migrationsFS)
```

to:

```go
db, err := database.Open(ws.DBPath(), database.Options{
	MaxReaders: cfg.Database.MaxReaders,
}, migrationsFS)
```

- [ ] **Step 2: Run app tests**

Run: `cd server && go test ./internal/app/ -v -count=1`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add server/internal/app/server.go
git commit -m "app: thread database.max_readers config into Open()"
```

---

### Task 13: Fix any remaining compilation errors and run full test suite

**Files:**
- Any remaining files that reference the old `db.Exec/db.Query/db.QueryRow/db.Begin` directly on `*database.DB`

- [ ] **Step 1: Compile check**

Run: `cd server && go build ./...`
Expected: no errors. If there are errors, they will point to remaining call sites that need migration.

- [ ] **Step 2: Fix any remaining call sites**

If the build fails, the errors will be of the form "db.Exec undefined (type *database.DB has no field or method Exec)". Each one indicates a call site that needs to be migrated to `db.Reader()` or `db.Writer()`.

- [ ] **Step 3: Run vet**

Run: `cd server && go vet ./...`
Expected: no issues

- [ ] **Step 4: Run full test suite**

Run: `cd server && go test ./... -count=1`
Expected: all PASS

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "fix: migrate remaining call sites to reader/writer pools"
```
