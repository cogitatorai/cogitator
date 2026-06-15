# Per-turn retrieval trace + retrieval metrics — Design

**Date:** 2026-06-15
**Scope:** IMPROVEMENTS.md §5.3 (per-turn retrieval trace; retrieval metrics). The
threshold tuning bullet of §5.3, the dashboard "memories used" widget, and the
eval baselines of §5.2 are explicitly out of scope (separate later work).

## Problem

Users report that the agent "does not always use relevant memories." Today this
is unknowable per turn: the vector retrieval path logs only counts, not node IDs
or scores (`internal/memory/retriever.go:425-431`). We cannot tell whether a
memory was never retrieved (below `MinSimilarity` 0.3, outside top-K, evicted by
the token budget), or retrieved-but-ignored by the model. We must instrument
before we can tune.

This design delivers two things:

1. A **config-gated, metadata-only retrieval trace** per chat turn (admin
   diagnostic), kept in a bounded in-memory ring and exposed via an admin API,
   plus an on-demand inline trace via a `?debug` path on `/api/chat`.
2. **Always-on aggregate retrieval metrics** surfaced in `/api/status`, carrying
   no sensitive content, so fleet-level drift (especially zero-retrieval rate)
   is visible without enabling the trace.

## Current state (grounding)

- `Retriever.Retrieve(ctx, userID, message, history) (*RetrievedContext, error)`
  (`retriever.go:233`) dispatches to `retrieveVector` (`:263`) or `retrieveLLM`
  (`:438`). Both compute per-candidate similarity/score and apply drop logic
  (MinSimilarity `:334-357`, greedy token-budget fill `:365-374`, pinned bypass
  `:310-324`) but return only `RetrievedContext{Pinned, Nodes, Connected}` — the
  scores and drop reasons are discarded. Current logging is count-only
  (`:425-431`).
- The agent reaches retrieval via `RetrieverAdapter.Retrieve` which flattens
  `RetrievedContext` to a formatted string (`internal/agent/retrieveradapter.go`);
  the agent injects it at `internal/agent/context.go:354-361`. Retrieval is
  invoked inside `agent.Chat` (`agent.go:401-412`), which is the single path used
  by HTTP chat, WebSocket, Telegram, and the task executor.
- Request IDs exist (`RequestIDFromContext`, `internal/api/requestlog.go:28-31`)
  but the context key is private to `internal/api`, so `internal/memory` cannot
  read it today.
- `requireRole(...)` (`internal/api/auth.go:90-108`) gates admin endpoints;
  `/api/status` (`internal/api/system.go`) already serializes the metrics ring
  `Snapshot` under `"http"`.

## Architecture

### Data model (`internal/memory`)

```go
// RetrievalTrace is the per-turn diagnostic record. Metadata only: no memory
// body text and no query text are stored, to keep the ring cheap and low-sensitivity.
type RetrievalTrace struct {
	RequestID   string
	SessionKey  string
	UserID      string
	Path        string   // "vector" | "llm-fallback"
	QueryChars  int      // length of the embedding/query text; the text itself is never stored
	Budget      int      // token budget (ContextWindow)
	TokensUsed  int
	Candidates  []TraceCandidate
	InjectedIDs []string
	PinnedIDs   []string
}

type TraceCandidate struct {
	NodeID     string
	Title      string
	Type       NodeType
	Similarity float64 // raw cosine (vector path; 0 on the llm path)
	Score      float64 // sim * confidence * typeBoost
	EstTokens  int
	Injected   bool
	DropReason string // "" (injected) | "below_min_similarity" | "token_budget" | "llm_not_selected" | "outside_top_k"
}
```

Drop reasons map to the actual selection stages: the vector path produces
`below_min_similarity` and `token_budget` (and `outside_top_k` only if a top-K cap
is confirmed in `retrieveVector` during implementation); the llm-fallback path
produces `llm_not_selected`. Pinned nodes are recorded in `PinnedIDs` and never
carry a drop reason.

`Retrieve` attaches `*RetrievalTrace` to `RetrievedContext` (a new `Trace` field),
nil unless a trace was requested (see gating). The struct is built from data the
retriever already has in hand during selection.

### Recording: record-at-source

Retrieval is recorded where it happens (the retriever), so every caller (HTTP,
WS, Telegram, tasks) is covered with no change to the agent↔retriever interface.

- New low-level package `internal/reqctx`: `WithRequestID(ctx, id) / RequestID(ctx) string`
  and `WithSessionKey / SessionKey`. `internal/api/requestlog.go` is updated to
  store the request ID via `reqctx` (replacing its private key); the chat entry
  points put the session key into the context. This removes the import-cycle
  barrier that currently keeps `internal/memory` from reading the request ID.
- The `Retriever` gains an optional `TraceSink` interface with one method
  `Record(*RetrievalTrace)`, set at construction. The bounded in-memory ring (see
  below) implements it.
- The trace is **built only when needed**: when config gating is on, or when
  `reqctx.TraceRequested(ctx)` is set by the `?debug` path. When neither is true,
  no `RetrievalTrace` is allocated — zero hot-path overhead by default.
- When gating is on, the retriever calls `sink.Record(trace)` after selection.
  The `?debug` path does not record into the ring (it returns inline only).

### Trace ring (`internal/memory`)

A bounded ring (default 200 entries) of the most recent `RetrievalTrace` values,
mirroring the existing metrics ring pattern. It lives in `internal/memory` because
it owns the `RetrievalTrace` type (so `internal/metrics` need not depend on
`internal/memory`). Thread-safe. `Snapshot()` returns a copy; optional filtering
by session/user is done by the API handler. Empty unless the config flag is on.

### Config gating

- `config.Memory.RetrievalTrace bool`, env override `COGITATOR_RETRIEVAL_TRACE`
  (mirrors the `COGITATOR_LOG_LEVEL`/`COGITATOR_LOG_FORMAT` pattern). Off by
  default. When off, the ring stays empty and the retriever skips trace
  construction except for explicit `?debug` requests.

### Aggregate metrics (always-on, `internal/metrics`)

A new `RetrievalStats` aggregator, cheap and always recorded (numbers only):

- A small ring of per-turn samples for distribution stats: top-similarity score,
  nodes injected, budget utilization.
- A turn counter and a zero-retrieval counter.

Surfaced in `/api/status` under `"retrieval"`:

```json
"retrieval": {
  "turns": 1234,
  "zero_retrieval_rate": 0.07,
  "top_similarity": { "p50": 0.61, "p95": 0.83, "avg": 0.58 },
  "avg_injected": 3.2,
  "avg_budget_util": 0.74
}
```

This is updated on every retrieval regardless of the trace flag, so the
zero-retrieval-rate drift signal is always available.

### Exposure

- **Admin API:** `GET /api/admin/retrieval-traces`, gated by `requireRole("admin")`,
  returns the recent-traces ring (newest first), optional `?session=` / `?user=`
  filters. Returns an empty list (not an error) when the flag is off.
- **`/api/chat` inline debug:** an admin-only `?debug=retrieval` query parameter
  sets `reqctx.TraceRequested` for that turn (chosen over a body field so the chat
  request schema is untouched); the handler returns the computed `RetrievalTrace`
  in the chat response. Works regardless of the global flag (explicit per-request
  admin opt-in). Non-admins requesting debug are ignored (no trace, no error).
- **Logging:** one slog line per turn under the request ID with summary numbers
  (top score, injected count, zero-retrieval bool) at debug level. Cheap, always
  emitted, replaces/augments the existing count-only line at `retriever.go:425`.

## Error handling

- A nil `TraceSink` or nil aggregator is a no-op (the retriever works without
  instrumentation, e.g. in the eval harness and unit tests).
- Trace construction never affects retrieval results or returns errors; a failure
  to record is logged at debug and swallowed (diagnostics must not break chat).
- `reqctx.RequestID`/`SessionKey` return "" when absent (non-HTTP callers); the
  trace still records with empty correlators.

## Testing

- `internal/memory`: table-driven tests asserting `DropReason` classification for
  the reasons the vector path actually produces (below-min-similarity,
  token-budget eviction, and outside-top-K if present) and injected-set
  correctness against seeded candidates with known embeddings; a test that the
  trace is nil when not requested and populated when requested.
- `internal/metrics`: `RetrievalStats` percentile and zero-retrieval-rate math;
  empty-state returns zero values.
- `internal/api`: `/api/admin/retrieval-traces` requires admin (403 for
  non-admin), returns the ring contents; `/api/chat` `?debug` returns an inline
  trace for an admin and omits it for a non-admin.
- `internal/reqctx`: round-trip get/set and empty-context defaults.
- Full suite green under `-race` (the ring is touched by concurrent retrievals).

## Out of scope (separate work)

- Dashboard "memories used in this reply" widget (§5.3 user-trust feature; frontend).
- DB persistence / historical analysis of traces.
- Threshold tuning experiments (`MinSimilarity`, `ContextWindow`, `TypeBoost`) —
  this design only provides the evidence to tune with (§5.3 last bullet).
- Eval baselines + regression gate (§5.2).

## N-1 / multi-environment notes

- Pure additive change: new config field (defaults off), new optional retriever
  dependencies (nil-safe), new always-additive JSON keys in `/api/status` and the
  chat response. No schema/migration. Safe under the N-1 rollback rule and
  identical across CLI/desktop/SaaS builds.
