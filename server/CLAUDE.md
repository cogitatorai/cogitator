# Cogitator Server

Go backend for all deployment targets (CLI, Desktop, SaaS).

## Entry Points

- `cmd/cogitator/main.go`: CLI binary. Loads config, creates `app.New()`, waits for signal.
- `cmd/cogitator-desktop/main.go`: Desktop binary (`//go:build desktop`). Embeds dashboard via `dashboard.FS()`.

## Build

```bash
# CLI (default)
go build -ldflags '-X .../version.Version=$(VERSION)' -o bin/cogitator ./cmd/cogitator

# Desktop (embeds dashboard)
go build -tags desktop -ldflags '...' -o bin/cogitator-desktop ./cmd/cogitator-desktop

# SaaS tenant
go build -tags saas -ldflags '...' -o bin/cogitator-tenant ./cmd/cogitator
```

## Test

```bash
go test ./... -count=1
go vet ./...
```

## Architecture

Bootstrap: `app.New(Options)` wires all subsystems into `Server`. `Server.Start()` listens HTTP.

Key subsystems (all in `internal/`):
- **api/**: HTTP router and handlers. Standard `http.Handler`, no framework. `Router` holds all subsystem refs.
- **agent/**: Core conversation loop. Calls LLM with tools, iterates until done, persists messages, emits events.
- **database/**: SQLite with WAL mode. Schema in `migrations.go` (idempotent). Numbered migrations in `migrations/`.
- **bus/**: In-process event bus. Non-blocking publish, buffered subscriber channels.
- **memory/**: Knowledge graph (nodes + edges). Vector embeddings for retrieval, recency boosting.
- **session/**: Chat session and message persistence. Scoped by session key + user ID.
- **task/**: Cron-based scheduled tasks. `Scheduler` loads tasks, `Executor` runs them via agent.
- **tools/**: Tool registry and dispatch. Built-in (23 tools), MCP, connectors, custom YAML tools.
- **config/**: YAML config with env var overrides. Secrets in OS keychain (fallback: `secrets.yaml`).
- **auth/**: JWT access + refresh tokens. Roles: admin, moderator, user.
- **provider/**: LLM abstraction. `Provider` interface with OpenAI-compatible implementation.
- **security/**: Shell sandboxing (host or Docker mode). Command blacklist, path filtering.
- **workspace/**: File system layout (`~/.cogitator`). Creates subdirs for memories, skills, tools.
- **mcp/**: MCP server lifecycle (stdio + SSE). Tool discovery and context enrichment.
- **skills/**: ClawHub search and install. Skills stored in workspace, indexed in memory.
- **connector/**: OAuth integrations (Google Calendar, Gmail). Token storage in keychain.
- **worker/**: Background workers: enricher (3 concurrent), profiler, consolidator, reflector.
- **channel/**: Chat transports: WebSocket (`/ws`), Telegram bot.

## Key Interfaces

- `provider.Provider`: LLM abstraction (pluggable)
- `tools.Executor`: Tool dispatch (built-in, MCP, connectors, custom)
- `memory.Retriever`: Knowledge graph recall (vector, LLM-fallback)
- `security.Runner`: Shell execution (host, docker)

## Conventions

- Error wrapping: `fmt.Errorf("context: %w", err)`
- Logging: `log/slog`
- HTTP: standard library, no framework
- Database: raw SQL, no ORM. Nullable types for scanning.
- Tests: table-driven, `t.Helper()`, `t.Cleanup()`, `httptest.NewRecorder()`
- Test setup: `setupTestRouter()` pattern with tempdir DB, mock provider, real bus
