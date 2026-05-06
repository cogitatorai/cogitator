# SQLite Reader/Writer Pool Split

## Problem

The database uses a single `sql.DB` with `MaxOpenConns(1)`, serializing all reads and writes
through one connection. SQLite in WAL mode supports concurrent readers alongside a single writer,
but the current configuration never exploits this. Under load with multiple concurrent users and
active background workers (enrichment, consolidation, reflection, profiling), this becomes a
latency bottleneck.

## Solution

Split the single `*sql.DB` into two pools within the `database.DB` struct:

- **Writer pool**: `MaxOpenConns(1)`. All INSERT, UPDATE, DELETE, and transactions.
- **Reader pool**: `MaxOpenConns(N)` (configurable, default 4). All SELECT queries. Opened with
  `_pragma=query_only(on)` to enforce read-only access at the SQLite level.

## DB Struct

```go
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
```

The embedded `*sql.DB` is removed. Callers must explicitly choose `Reader()` or `Writer()`.

## Open() Signature

```go
type Options struct {
    MaxReaders int // 0 means use default (4)
}

func Open(path string, opts Options, migrationsFS ...fs.FS) (*DB, error)
```

Connection strings:

```
Writer: path?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)
Reader: path?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=query_only(on)
```

Writer pool: `SetMaxOpenConns(1)`.
Reader pool: `SetMaxOpenConns(opts.MaxReaders)` with fallback to 4 when 0.

Migrations run on the writer pool only.

The `Open()` signature changes from `Open(path string, migrationsFS ...fs.FS)` to
`Open(path string, opts Options, migrationsFS ...fs.FS)`. The single call site in
`internal/app/server.go` is updated accordingly.

## Configuration

New `database` section in `cogitator.yaml`:

```yaml
database:
  max_readers: 4
```

Threaded through `config.Config` into the `database.Open()` call in `app.New()`.

## Caller Migration

Mechanical changes across all stores and direct DB callers:

| Operation | Pool | Method |
|-----------|------|--------|
| SELECT (Query, QueryRow, QueryContext, QueryRowContext) | Reader | `s.db.Reader().QueryContext(...)` |
| INSERT, UPDATE, DELETE (Exec, ExecContext) | Writer | `s.db.Writer().ExecContext(...)` |
| Transactions (Begin, BeginTx) | Writer | `s.db.Writer().Begin()` |

Affected packages:
- `internal/memory` (Store: 21 reads, 14 writes, 1 transaction)
- `internal/session` (Store: 10 reads, 13 writes)
- `internal/task` (Store: 12 reads, 15 writes)
- `internal/user` (Store: 10 reads, 17 writes)
- `internal/notification` (Store: 3 reads, 6 writes)
- `internal/push` (Store: 2 reads, 3 writes)
- `internal/database` (audit: 2 reads + 1 write, usage: 1 read + 1 write)
- `internal/api` (Router direct access: audit reads, usage reads, user handler write)
- `internal/worker` (backfill: mixed via `store.DB()`)

The `memory.Store.DB()` accessor continues to return `*database.DB`. Backfill worker uses
`store.DB().Reader()` for reads and `store.DB().Writer()` for writes.

## Transactions

Only two transaction sites exist, both write-only:
1. Migration runner (`internal/database/migration_runner.go`): DDL + version recording.
2. `SetNodeVisibility` (`internal/memory/store.go`): cascading UPDATE on nodes and edges.

Both use `db.Writer().Begin()`. No read-write mixed transactions exist.

## Lifecycle

**Startup order**: Writer pool created first, migrations run on it, reader pool created second.
If reader pool creation fails, writer is closed before returning.

**Shutdown order**: Reader closed first (drains in-flight reads), writer closed second. Both
errors joined and returned.

## Testing

Three new tests in `internal/database/`:

1. **Concurrency test**: Open a write transaction on `Writer()`, fire concurrent reads on
   `Reader()`, assert reads complete while the transaction is still open. Validates WAL mode
   concurrent access works through the pool split.

2. **Read-only enforcement test**: Issue an INSERT through `Reader()`, assert it returns an
   error. Validates the `query_only` pragma.

3. **Close test**: Call `Close()`, verify subsequent operations on both `Reader()` and `Writer()`
   return errors.

## Future: PostgreSQL Migration

The `Reader()`/`Writer()` accessor pattern maps directly to read-replica routing. When migrating
to PostgreSQL:
- Both accessors can point to the same `*sql.DB` pool (single-line change).
- Or `Reader()` can point to a read replica for horizontal read scaling.
- Call-site annotations of intent (read vs write) remain valuable regardless of backend.
