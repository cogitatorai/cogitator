# Per-turn Retrieval Trace + Retrieval Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make "why didn't the agent use my memory?" answerable per chat turn via a config-gated, metadata-only retrieval trace (admin diagnostic) plus always-on aggregate retrieval metrics surfaced in `/api/status`.

**Architecture:** Record-at-source. `internal/memory.Retriever` builds a `RetrievalTrace` (candidates, similarity, drop reasons, injected set) during selection, records it into a bounded in-memory `TraceRing` when a config flag is on, and feeds always-on aggregate `metrics.RetrievalStats`. Request IDs move to a new `internal/reqctx` package so `internal/memory` can read them without importing `internal/api`. An admin API lists the ring; an admin `?debug=retrieval` query param on `/api/chat` returns the live turn's trace inline via a context-planted holder (the same pattern `requestlog.go` uses for `routeInfo`).

**Tech Stack:** Go 1.25 stdlib (`context`, `log/slog`, `net/http`, `sort`, `math`, `sync`), table-driven tests with `httptest`.

**Conventions that apply to every task:**
- Run all Go commands from `/Users/deiu/dev/deiu/cogi/cogitator/server`.
- Commit messages: lowercase `area: summary`. NEVER add a `Co-Authored-By` trailer (user rule).
- Error wrapping `fmt.Errorf("context: %w", err)`; logging via `log/slog`; table-driven tests.
- Branch is already `feat/retrieval-trace` (created off `main`). The deleted `docs/superpowers/*` sqlite files and untracked `IMPROVEMENTS.md` in the working tree are pre-existing; leave them alone.
- Trace is **metadata only**: never store memory body text or query text.

---

### Task 1: `internal/reqctx` package + integrate into request logging (§ recording correlation)

**Files:**
- Create: `internal/reqctx/reqctx.go`
- Test: `internal/reqctx/reqctx_test.go`
- Modify: `internal/api/requestlog.go`

- [ ] **Step 1: Write the failing test**

Create `internal/reqctx/reqctx_test.go`:

```go
package reqctx

import (
	"context"
	"testing"
)

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	if got := RequestID(ctx); got != "req-123" {
		t.Errorf("RequestID = %q, want req-123", got)
	}
}

func TestSessionKeyRoundTrip(t *testing.T) {
	ctx := WithSessionKey(context.Background(), "web:default")
	if got := SessionKey(ctx); got != "web:default" {
		t.Errorf("SessionKey = %q, want web:default", got)
	}
}

func TestEmptyContextDefaults(t *testing.T) {
	if got := RequestID(context.Background()); got != "" {
		t.Errorf("RequestID on empty ctx = %q, want empty", got)
	}
	if got := SessionKey(context.Background()); got != "" {
		t.Errorf("SessionKey on empty ctx = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/reqctx/
```

Expected: FAIL (package does not compile, `WithRequestID` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/reqctx/reqctx.go`:

```go
// Package reqctx carries request-scoped correlation values (request ID,
// session key) in a context.Context. It lives in its own low-level package so
// any layer (api middleware, memory retriever, agent) can read or set them
// without import cycles.
package reqctx

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	sessionKeyKey
)

// WithRequestID returns a context carrying the per-request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID, or "" when absent.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithSessionKey returns a context carrying the chat session key.
func WithSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, sessionKeyKey, key)
}

// SessionKey returns the session key, or "" when absent.
func SessionKey(ctx context.Context) string {
	k, _ := ctx.Value(sessionKeyKey).(string)
	return k
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/reqctx/
```

Expected: `ok ... internal/reqctx`.

- [ ] **Step 5: Route the request ID through reqctx in requestlog.go**

In `internal/api/requestlog.go`, the middleware currently stores the request ID under its own private `requestIDKey`. Change it to also store it via `reqctx` so the retriever can read it, and make `RequestIDFromContext` delegate.

Add the import `"github.com/cogitatorai/cogitator/server/internal/reqctx"`.

In `requestLogMiddleware`, where the context is built (the lines that do `ctx := context.WithValue(r.Context(), requestIDKey, id)`), add immediately after:

```go
		ctx = reqctx.WithRequestID(ctx, id)
```

Change `RequestIDFromContext` to delegate (keep the function so existing callers/tests compile):

```go
// RequestIDFromContext returns the per-request ULID, or "" when absent.
func RequestIDFromContext(ctx context.Context) string {
	return reqctx.RequestID(ctx)
}
```

- [ ] **Step 6: Build + run api/metrics tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./... && go test ./internal/api/ ./internal/reqctx/ -count=1
```

Expected: PASS (existing `requestlog_test.go` still green; request ID now also in reqctx).

- [ ] **Step 7: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/reqctx/ server/internal/api/requestlog.go
git commit -m "reqctx: add request-scoped correlation package; route request ID through it"
```

---

### Task 2: `metrics.RetrievalStats` aggregator (§1.2 aggregate metrics, always-on)

**Files:**
- Create: `internal/metrics/retrieval.go`
- Test: `internal/metrics/retrieval_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/retrieval_test.go`:

```go
package metrics

import "testing"

func TestRetrievalStatsSnapshot(t *testing.T) {
	s := NewRetrievalStats(10)
	// topSim, injected, budgetUtil
	s.Record(0.9, 3, 0.5)
	s.Record(0.5, 0, 0.0) // zero-retrieval turn
	s.Record(0.7, 2, 1.0)

	snap := s.Snapshot()
	if snap.Turns != 3 {
		t.Errorf("Turns = %d, want 3", snap.Turns)
	}
	wantZero := 1.0 / 3.0
	if snap.ZeroRetrievalRate < wantZero-0.001 || snap.ZeroRetrievalRate > wantZero+0.001 {
		t.Errorf("ZeroRetrievalRate = %v, want ~%v", snap.ZeroRetrievalRate, wantZero)
	}
	if snap.TopSimilarity.Avg < 0.69 || snap.TopSimilarity.Avg > 0.71 {
		t.Errorf("TopSimilarity.Avg = %v, want ~0.70", snap.TopSimilarity.Avg)
	}
	if snap.TopSimilarity.P95 < 0.89 || snap.TopSimilarity.P95 > 0.91 {
		t.Errorf("TopSimilarity.P95 = %v, want ~0.90", snap.TopSimilarity.P95)
	}
	if snap.AvgInjected < 1.66 || snap.AvgInjected > 1.67 {
		t.Errorf("AvgInjected = %v, want ~1.667", snap.AvgInjected)
	}
	if snap.AvgBudgetUtil < 0.49 || snap.AvgBudgetUtil > 0.51 {
		t.Errorf("AvgBudgetUtil = %v, want ~0.50", snap.AvgBudgetUtil)
	}
}

func TestRetrievalStatsEmpty(t *testing.T) {
	s := NewRetrievalStats(10)
	snap := s.Snapshot()
	if snap.Turns != 0 || snap.ZeroRetrievalRate != 0 || snap.TopSimilarity.Avg != 0 {
		t.Errorf("empty snapshot non-zero: %+v", snap)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/metrics/ -run TestRetrievalStats
```

Expected: FAIL to compile (`NewRetrievalStats` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/metrics/retrieval.go`:

```go
package metrics

import (
	"math"
	"sort"
	"sync"
)

// RetrievalStats aggregates per-turn memory-retrieval signals. Turn and
// zero-retrieval counts are cumulative; the distribution stats (top similarity,
// injected count, budget utilization) are computed over a bounded ring of the
// most recent samples. All values are numeric — no memory content — so this is
// always-on. Only the vector retrieval path records here (similarity is
// meaningless on the llm-fallback path).
type RetrievalStats struct {
	mu         sync.Mutex
	size       int
	pos        int
	full       bool
	topSim     []float64
	injected   []int
	budgetUtil []float64
	turns      int
	zero       int
}

// NewRetrievalStats creates a stats aggregator keeping the last size samples.
func NewRetrievalStats(size int) *RetrievalStats {
	if size <= 0 {
		size = 1000
	}
	return &RetrievalStats{
		size:       size,
		topSim:     make([]float64, size),
		injected:   make([]int, size),
		budgetUtil: make([]float64, size),
	}
}

// Record adds one retrieval turn. topSim is the highest candidate cosine
// similarity seen that turn (regardless of threshold), injected is the number
// of nodes injected, budgetUtil is tokensUsed/budget in [0,1].
func (s *RetrievalStats) Record(topSim float64, injected int, budgetUtil float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns++
	if injected == 0 {
		s.zero++
	}
	s.topSim[s.pos] = topSim
	s.injected[s.pos] = injected
	s.budgetUtil[s.pos] = budgetUtil
	s.pos++
	if s.pos >= s.size {
		s.pos = 0
		s.full = true
	}
}

// Percentiles holds a small distribution summary.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Avg float64 `json:"avg"`
}

// RetrievalSnapshot is the JSON-serializable view surfaced in /api/status.
type RetrievalSnapshot struct {
	Turns             int         `json:"turns"`
	ZeroRetrievalRate float64     `json:"zero_retrieval_rate"`
	TopSimilarity     Percentiles `json:"top_similarity"`
	AvgInjected       float64     `json:"avg_injected"`
	AvgBudgetUtil     float64     `json:"avg_budget_util"`
}

// Snapshot returns a point-in-time summary. Cumulative counters cover all turns;
// distribution stats cover the recent sample ring.
func (s *RetrievalStats) Snapshot() RetrievalSnapshot {
	s.mu.Lock()
	n := s.pos
	if s.full {
		n = s.size
	}
	turns := s.turns
	zero := s.zero
	sims := make([]float64, n)
	var sumInj, sumBudget float64
	for i := 0; i < n; i++ {
		sims[i] = s.topSim[i]
		sumInj += float64(s.injected[i])
		sumBudget += s.budgetUtil[i]
	}
	s.mu.Unlock()

	snap := RetrievalSnapshot{Turns: turns}
	if turns > 0 {
		snap.ZeroRetrievalRate = float64(zero) / float64(turns)
	}
	if n == 0 {
		return snap
	}
	sorted := make([]float64, n)
	copy(sorted, sims)
	sort.Float64s(sorted)
	var sumSim float64
	for _, v := range sorted {
		sumSim += v
	}
	snap.TopSimilarity = Percentiles{
		P50: sorted[pctIndex(n, 0.50)],
		P95: sorted[pctIndex(n, 0.95)],
		Avg: sumSim / float64(n),
	}
	snap.AvgInjected = sumInj / float64(n)
	snap.AvgBudgetUtil = sumBudget / float64(n)
	return snap
}

// pctIndex returns the index of the p-th percentile in a sorted slice of n items.
func pctIndex(n int, p float64) int {
	idx := int(math.Ceil(float64(n)*p)) - 1
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/metrics/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/metrics/retrieval.go server/internal/metrics/retrieval_test.go
git commit -m "metrics: add always-on RetrievalStats aggregator"
```

---

### Task 3: Trace types, ring, sinks, and context holder (`internal/memory`)

**Files:**
- Create: `internal/memory/trace.go`
- Test: `internal/memory/trace_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/memory/trace_test.go`:

```go
package memory

import (
	"context"
	"testing"
)

func TestTraceRingNewestFirst(t *testing.T) {
	r := NewTraceRing(2)
	r.Record(&RetrievalTrace{RequestID: "a"})
	r.Record(&RetrievalTrace{RequestID: "b"})
	r.Record(&RetrievalTrace{RequestID: "c"}) // evicts "a"

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("len = %d, want 2", len(snap))
	}
	if snap[0].RequestID != "c" || snap[1].RequestID != "b" {
		t.Errorf("order = %q,%q; want c,b", snap[0].RequestID, snap[1].RequestID)
	}
}

func TestTraceRingEmpty(t *testing.T) {
	r := NewTraceRing(4)
	if got := r.Snapshot(); len(got) != 0 {
		t.Errorf("empty ring snapshot len = %d, want 0", len(got))
	}
}

func TestTraceHolderViaContext(t *testing.T) {
	ctx, holder := WithTrace(context.Background())
	if traceHolderFrom(ctx) == nil {
		t.Fatal("holder not planted in context")
	}
	tr := &RetrievalTrace{RequestID: "x"}
	traceHolderFrom(ctx).Set(tr)
	if holder.Get() != tr {
		t.Error("holder did not capture the trace")
	}
}

func TestTraceHolderAbsent(t *testing.T) {
	if traceHolderFrom(context.Background()) != nil {
		t.Error("expected nil holder on bare context")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run 'TestTrace'
```

Expected: FAIL to compile (`NewTraceRing`, `RetrievalTrace`, `WithTrace` undefined).

- [ ] **Step 3: Write the implementation**

Create `internal/memory/trace.go`:

```go
package memory

import (
	"context"
	"sync"
)

// RetrievalTrace is the per-turn diagnostic record. Metadata only: no memory
// body text and no query text are stored.
type RetrievalTrace struct {
	RequestID   string           `json:"request_id"`
	SessionKey  string           `json:"session_key"`
	UserID      string           `json:"user_id"`
	Path        string           `json:"path"` // "vector" | "llm-fallback"
	QueryChars  int              `json:"query_chars"`
	Budget      int              `json:"budget"`
	TokensUsed  int              `json:"tokens_used"`
	Candidates  []TraceCandidate `json:"candidates"`
	InjectedIDs []string         `json:"injected_ids"`
	PinnedIDs   []string         `json:"pinned_ids"`
}

// TraceCandidate is one scored candidate node and its disposition.
type TraceCandidate struct {
	NodeID     string   `json:"node_id"`
	Title      string   `json:"title,omitempty"` // set only for nodes we already loaded
	Type       NodeType `json:"type"`
	Similarity float64  `json:"similarity"` // raw cosine (vector path; 0 on llm path)
	Score      float64  `json:"score"`      // sim * confidence * typeBoost
	EstTokens  int      `json:"est_tokens"`
	Injected   bool     `json:"injected"`
	DropReason string   `json:"drop_reason,omitempty"`
}

// Drop reason constants.
const (
	DropBelowMinSimilarity = "below_min_similarity"
	DropTokenBudget        = "token_budget"
	DropOutsideTopK        = "outside_top_k"
	DropLLMNotSelected     = "llm_not_selected"
)

// TraceSink receives completed traces (implemented by *TraceRing).
type TraceSink interface {
	Record(*RetrievalTrace)
}

// StatsSink receives per-turn aggregate samples (implemented by
// *metrics.RetrievalStats). Defined here so internal/memory does not import
// internal/metrics.
type StatsSink interface {
	Record(topSim float64, injected int, budgetUtil float64)
}

// TraceRing is a bounded, thread-safe ring of the most recent traces.
type TraceRing struct {
	mu      sync.Mutex
	size    int
	pos     int
	full    bool
	entries []*RetrievalTrace
}

// NewTraceRing creates a ring holding the last size traces.
func NewTraceRing(size int) *TraceRing {
	if size <= 0 {
		size = 200
	}
	return &TraceRing{size: size, entries: make([]*RetrievalTrace, size)}
}

// Record stores a trace, evicting the oldest when full.
func (r *TraceRing) Record(t *RetrievalTrace) {
	if t == nil {
		return
	}
	r.mu.Lock()
	r.entries[r.pos] = t
	r.pos++
	if r.pos >= r.size {
		r.pos = 0
		r.full = true
	}
	r.mu.Unlock()
}

// Snapshot returns the stored traces, newest first.
func (r *TraceRing) Snapshot() []*RetrievalTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.pos
	if r.full {
		n = r.size
	}
	out := make([]*RetrievalTrace, 0, n)
	// Walk backwards from the most recently written slot.
	for i := 0; i < n; i++ {
		idx := (r.pos - 1 - i + r.size) % r.size
		if r.entries[idx] != nil {
			out = append(out, r.entries[idx])
		}
	}
	return out
}

// TraceHolder captures a single trace for inline return (the ?debug path).
type TraceHolder struct {
	mu sync.Mutex
	tr *RetrievalTrace
}

// Set stores the trace.
func (h *TraceHolder) Set(t *RetrievalTrace) {
	h.mu.Lock()
	h.tr = t
	h.mu.Unlock()
}

// Get returns the captured trace, or nil.
func (h *TraceHolder) Get() *RetrievalTrace {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tr
}

type traceCtxKey int

const traceHolderKey traceCtxKey = iota

// WithTrace plants a TraceHolder in the context, requesting that the retriever
// build a trace for this turn even when the global trace flag is off. Returns
// the new context and the holder to read after retrieval.
func WithTrace(ctx context.Context) (context.Context, *TraceHolder) {
	h := &TraceHolder{}
	return context.WithValue(ctx, traceHolderKey, h), h
}

// traceHolderFrom returns the planted holder, or nil.
func traceHolderFrom(ctx context.Context) *TraceHolder {
	h, _ := ctx.Value(traceHolderKey).(*TraceHolder)
	return h
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run 'TestTrace' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/memory/trace.go server/internal/memory/trace_test.go
git commit -m "memory: add retrieval trace types, bounded ring, and context holder"
```

---

### Task 4: Instrument the vector retrieval path

**Files:**
- Modify: `internal/memory/retriever.go` (struct, `RetrieverConfig`, `NewRetriever`, `retrieveVector`)
- Test: `internal/memory/retriever_trace_test.go`

Design: `retrieveVector` already computes cosine similarity for every cached candidate and selects by token budget. We add three optional dependencies to the `Retriever` (`traceEnabled bool`, `traceSink TraceSink`, `stats StatsSink`), track the max similarity for stats (always), and build a `RetrievalTrace` only when tracing is on (config flag OR a context holder is present). Below-threshold candidates are sampled (highest similarity first, capped at 20) so a "just under 0.3" miss is visible without recording the whole graph.

- [ ] **Step 1: Write the failing test**

Create `internal/memory/retriever_trace_test.go`. This drives the new construction fields and the trace contents using the existing in-memory store test helpers. First inspect how existing retriever tests seed a store and embedder:

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
sed -n '1,60p' internal/memory/retriever_test.go
grep -n "func.*Store\|NewStore\|fakeEmbedder\|Embed(" internal/memory/retriever_test.go | head
```

Then write the test, reusing whatever store/embedder helpers the existing test file provides (named `newTestStore`/`fakeEmbedder` or similar — match the real names found above). The test must:

```go
package memory

import (
	"context"
	"testing"
)

// TestVectorTraceClassifiesDrops seeds nodes whose embeddings yield a known
// similarity ordering and asserts the trace records injected vs dropped with
// the right reasons. Use the same store + embedder helpers as retriever_test.go.
func TestVectorTraceClassifiesDrops(t *testing.T) {
	// Arrange: build a store with (at least) one high-similarity node that fits
	// the budget, and one below MinSimilarity. Construct the retriever with a
	// tiny TokenBudget so a second above-threshold node is dropped for budget.
	// Set TraceEnabled: true and a *TraceRing as TraceSink.
	//
	// Replace the helper calls below with the actual ones from retriever_test.go.
	store := newTestStore(t)            // <- match real helper name
	emb := newFakeEmbedder()            // <- match real helper name
	// ... seed nodes with embeddings via store + emb (mirror existing tests) ...

	ring := NewTraceRing(8)
	r := NewRetriever(RetrieverConfig{
		Store:         store,
		Embedder:      emb,
		MinSimilarity: 0.3,
		TokenBudget:   10, // force a budget drop
		TraceEnabled:  true,
		TraceSink:     ring,
	})

	_, err := r.Retrieve(context.Background(), "u1", "query text", nil)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	traces := ring.Snapshot()
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	tr := traces[0]
	if tr.Path != "vector" {
		t.Errorf("Path = %q, want vector", tr.Path)
	}
	var sawInjected, sawBudget, sawBelow bool
	for _, c := range tr.Candidates {
		switch {
		case c.Injected:
			sawInjected = true
		case c.DropReason == DropTokenBudget:
			sawBudget = true
		case c.DropReason == DropBelowMinSimilarity:
			sawBelow = true
		}
	}
	if !sawInjected || !sawBudget || !sawBelow {
		t.Errorf("missing dispositions: injected=%v budget=%v below=%v (candidates=%+v)",
			sawInjected, sawBudget, sawBelow, tr.Candidates)
	}
	if len(tr.InjectedIDs) == 0 {
		t.Error("InjectedIDs empty")
	}
}

// TestNoTraceWhenDisabled verifies zero overhead path: no flag, no holder => nil.
func TestNoTraceWhenDisabled(t *testing.T) {
	store := newTestStore(t)
	emb := newFakeEmbedder()
	// ... seed one matching node ...
	ring := NewTraceRing(8)
	r := NewRetriever(RetrieverConfig{
		Store: store, Embedder: emb, MinSimilarity: 0.3, TraceSink: ring,
		// TraceEnabled defaults false
	})
	if _, err := r.Retrieve(context.Background(), "u1", "q", nil); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got := ring.Snapshot(); len(got) != 0 {
		t.Errorf("ring recorded %d traces with flag off, want 0", len(got))
	}
}
```

> NOTE for the implementer: open `internal/memory/retriever_test.go` first and copy its exact store/embedder seeding pattern into the Arrange sections above. Do not invent helper names.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run 'TestVectorTrace|TestNoTraceWhenDisabled'
```

Expected: FAIL to compile (`RetrieverConfig.TraceEnabled`/`TraceSink` undefined).

- [ ] **Step 3: Add fields to the struct, config, and constructor**

In `internal/memory/retriever.go`, add to the `Retriever` struct (after `types []NodeType`):

```go
	// Instrumentation (all optional / nil-safe).
	traceEnabled bool
	traceSink    TraceSink
	stats        StatsSink
```

Add to `RetrieverConfig` (after `Types []NodeType`):

```go
	// Instrumentation. TraceEnabled gates recording into TraceSink; the trace
	// is also built on demand when a context holder is present (?debug). Stats
	// is always-on (vector path only). All optional.
	TraceEnabled bool
	TraceSink    TraceSink
	Stats        StatsSink
```

In `NewRetriever`, add to the returned `&Retriever{...}` literal:

```go
		traceEnabled: cfg.TraceEnabled,
		traceSink:    cfg.TraceSink,
		stats:        cfg.Stats,
```

- [ ] **Step 4: Instrument `retrieveVector`**

In `retrieveVector`, make these edits:

(a) Read the trace gate near the top, after the `r.mu.RUnlock()` that snapshots config:

```go
	holder := traceHolderFrom(ctx)
	traceOn := r.traceEnabled || holder != nil
```

(b) Extend the local `scored` struct to carry data the trace needs:

```go
	type scored struct {
		id            string
		score         float64
		sim           float64
		typ           NodeType
		contentLength int
	}
	var candidates []scored
	var belowThreshold []TraceCandidate
	var maxSim float64
```

(c) In the candidate scoring loop, track max similarity always, and collect below-threshold candidates when tracing. Replace the existing loop body around the `if sim < minSim { continue }` block with:

```go
	for id, meta := range cache {
		if seen[id] {
			continue
		}
		sim := CosineSimilarity(queryVec, meta.Embedding)
		if sim > maxSim {
			maxSim = sim
		}

		if sim < minSim {
			if traceOn {
				belowThreshold = append(belowThreshold, TraceCandidate{
					NodeID:     id,
					Type:       meta.Type,
					Similarity: sim,
					EstTokens:  estimateTokensFromLength(meta.ContentLength),
					DropReason: DropBelowMinSimilarity,
				})
			}
			continue
		}

		score := sim * meta.Confidence
		if meta.Type == NodePreference || meta.Type == NodeFact {
			score *= tBoost
		}
		candidates = append(candidates, scored{
			id:            id,
			score:         score,
			sim:           sim,
			typ:           meta.Type,
			contentLength: meta.ContentLength,
		})
	}
```

(d) The budget-fill loop already produces `selected`. After the loop that loads selected nodes (`result.Nodes` populated), build the trace and record stats. Add, right before the edge-following section (`// Follow 1-hop high-weight edges ...`):

```go
	// Always-on aggregate stats (vector path).
	if r.stats != nil {
		budgetUtil := 0.0
		if budget > 0 {
			budgetUtil = float64(tokensUsed) / float64(budget)
		}
		r.stats.Record(maxSim, len(result.Nodes), budgetUtil)
	}

	// Build the per-turn trace when requested.
	if traceOn {
		selectedIDs := make(map[string]bool, len(selected))
		for _, c := range selected {
			selectedIDs[c.id] = true
		}
		titleByID := make(map[string]string, len(result.Nodes))
		for _, rn := range result.Nodes {
			titleByID[rn.Node.ID] = rn.Node.Title
		}
		tr := &RetrievalTrace{
			RequestID:  reqctx.RequestID(ctx),
			SessionKey: reqctx.SessionKey(ctx),
			UserID:     userID,
			Path:       "vector",
			QueryChars: len(queryText),
			Budget:     budget,
			TokensUsed: tokensUsed,
		}
		for _, c := range candidates {
			tc := TraceCandidate{
				NodeID:     c.id,
				Title:      titleByID[c.id],
				Type:       c.typ,
				Similarity: c.sim,
				Score:      c.score,
				EstTokens:  estimateTokensFromLength(c.contentLength),
				Injected:   selectedIDs[c.id],
			}
			if tc.Injected {
				tr.InjectedIDs = append(tr.InjectedIDs, c.id)
			} else {
				tc.DropReason = DropTokenBudget
			}
			tr.Candidates = append(tr.Candidates, tc)
		}
		// Append a capped, highest-similarity sample of below-threshold drops.
		sort.Slice(belowThreshold, func(i, j int) bool {
			return belowThreshold[i].Similarity > belowThreshold[j].Similarity
		})
		const maxBelowTraced = 20
		if len(belowThreshold) > maxBelowTraced {
			belowThreshold = belowThreshold[:maxBelowTraced]
		}
		tr.Candidates = append(tr.Candidates, belowThreshold...)
		for _, pn := range result.Pinned {
			tr.PinnedIDs = append(tr.PinnedIDs, pn.Node.ID)
		}
		if holder != nil {
			holder.Set(tr)
		}
		if r.traceEnabled && r.traceSink != nil {
			r.traceSink.Record(tr)
		}
	}
```

(e) Add the import `"github.com/cogitatorai/cogitator/server/internal/reqctx"` to `retriever.go`.

(f) Replace the existing count-only log line (`r.logger.Info("retrieval: vector path", ...)` around line 425) with a debug-level summary that always emits:

```go
	r.logger.Debug("retrieval: vector path",
		"request_id", reqctx.RequestID(ctx),
		"query_len", len(queryText),
		"pinned", len(result.Pinned),
		"nodes", len(result.Nodes),
		"connected", len(result.Connected),
		"tokens_used", tokensUsed,
		"top_similarity", maxSim,
		"zero_retrieval", len(result.Nodes) == 0,
	)
```

- [ ] **Step 5: Run the trace tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run 'TestVectorTrace|TestNoTraceWhenDisabled' -count=1
```

Expected: PASS. If the budget-drop assertion fails, lower `TokenBudget` in the test or add a second above-threshold node so at least one is evicted.

- [ ] **Step 6: Run the whole memory package**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -count=1
```

Expected: PASS (existing retriever tests unaffected — new params default to off).

- [ ] **Step 7: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/memory/retriever.go server/internal/memory/retriever_trace_test.go
git commit -m "memory: instrument vector retrieval with trace + aggregate stats"
```

---

### Task 5: Instrument the llm-fallback retrieval path

**Files:**
- Modify: `internal/memory/retriever.go` (`retrieveLLM`)
- Test: `internal/memory/retriever_trace_test.go` (append)

Design: `retrieveLLM` has every node summary (id/type/title) and the LLM-selected, top-K-truncated `nodeIDs`. Build a trace marking selected nodes injected, truncated ones `outside_top_k`, and the rest `llm_not_selected`. Similarity is 0 (no vectors). Aggregate stats are NOT recorded here (similarity-based metrics are vector-only).

- [ ] **Step 1: Write the failing test**

Append to `internal/memory/retriever_trace_test.go`. Inspect how existing tests stub the provider for `retrieveLLM` first:

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
grep -n "retrieveLLM\|fakeProvider\|GetNodeSummaries\|Chat(" internal/memory/*_test.go | head
```

Then:

```go
func TestLLMTraceClassifiesDrops(t *testing.T) {
	store := newTestStore(t)
	// Seed >topK summaries so truncation happens; no embedder configured so the
	// retriever uses the llm path. Stub the provider to return a JSON array of
	// node IDs (match the fake-provider pattern from the existing tests).
	prov := newFakeProvider(/* returns ["<id1>","<id2>", ...] */)

	ring := NewTraceRing(8)
	r := NewRetriever(RetrieverConfig{
		Store:        store,
		Provider:     prov,
		Model:        "test",
		TopK:         1, // force outside_top_k
		TraceEnabled: true,
		TraceSink:    ring,
	})
	if _, err := r.Retrieve(context.Background(), "u1", "q", nil); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	traces := ring.Snapshot()
	if len(traces) != 1 || traces[0].Path != "llm-fallback" {
		t.Fatalf("traces=%d path=%v", len(traces), traces)
	}
	var sawInjected, sawTopK, sawNotSel bool
	for _, c := range traces[0].Candidates {
		switch {
		case c.Injected:
			sawInjected = true
		case c.DropReason == DropOutsideTopK:
			sawTopK = true
		case c.DropReason == DropLLMNotSelected:
			sawNotSel = true
		}
	}
	if !sawInjected || !sawTopK || !sawNotSel {
		t.Errorf("dispositions injected=%v topk=%v notsel=%v", sawInjected, sawTopK, sawNotSel)
	}
}
```

> NOTE: match `newFakeProvider`/`newTestStore` to the real helper names found by the grep above.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run TestLLMTrace
```

Expected: FAIL (trace not built in llm path; `Path` empty / no candidates).

- [ ] **Step 3: Instrument `retrieveLLM`**

In `retrieveLLM`, after `nodeIDs` is parsed and BEFORE the `if len(nodeIDs) > topK` truncation, capture the full selected order. Then after building `result.Nodes`, build the trace.

Capture the gate at the top (after the `r.mu.RUnlock()` snapshot):

```go
	holder := traceHolderFrom(ctx)
	traceOn := r.traceEnabled || holder != nil
```

After the truncation block (`if len(nodeIDs) > topK { nodeIDs = nodeIDs[:topK] }`) keep a copy of the pre-truncation list. Adjust to:

```go
	selectedAll := append([]string(nil), nodeIDs...) // full LLM order, pre-truncation
	if len(nodeIDs) > topK {
		nodeIDs = nodeIDs[:topK]
	}
```

Replace the existing `r.logger.Info("retrieval: LLM path nodes selected", ...)` line with a debug summary:

```go
	r.logger.Debug("retrieval: llm path",
		"request_id", reqctx.RequestID(ctx),
		"selected", len(nodeIDs),
	)
```

After the edge-following section, just before `return result, nil`, add:

```go
	if traceOn {
		injected := make(map[string]bool, len(nodeIDs))
		for _, id := range nodeIDs {
			injected[id] = true
		}
		truncated := make(map[string]bool)
		for _, id := range selectedAll {
			if !injected[id] {
				truncated[id] = true
			}
		}
		tr := &RetrievalTrace{
			RequestID:  reqctx.RequestID(ctx),
			SessionKey: reqctx.SessionKey(ctx),
			UserID:     userID,
			Path:       "llm-fallback",
			QueryChars: len(message),
		}
		for _, s := range summaries {
			tc := TraceCandidate{
				NodeID:   s.ID,
				Title:    s.Title,
				Type:     s.Type,
				Injected: injected[s.ID],
			}
			switch {
			case tc.Injected:
				tr.InjectedIDs = append(tr.InjectedIDs, s.ID)
			case truncated[s.ID]:
				tc.DropReason = DropOutsideTopK
			default:
				tc.DropReason = DropLLMNotSelected
			}
			tr.Candidates = append(tr.Candidates, tc)
		}
		if holder != nil {
			holder.Set(tr)
		}
		if r.traceEnabled && r.traceSink != nil {
			r.traceSink.Record(tr)
		}
	}
```

> Verify `summaries` (the slice from `GetNodeSummaries`) and its element fields (`ID`, `Type`, `Title`) are in scope at the return point; they are declared near the top of `retrieveLLM`. If the variable was named differently, use the real name.

- [ ] **Step 4: Run the llm trace test**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/memory/ -run TestLLMTrace -count=1
```

Expected: PASS.

- [ ] **Step 5: Run the whole memory package + vet**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go vet ./internal/memory/ && go test ./internal/memory/ -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/memory/retriever.go server/internal/memory/retriever_trace_test.go
git commit -m "memory: instrument llm-fallback retrieval with trace"
```

---

### Task 6: Config flag + app wiring

**Files:**
- Modify: `internal/config/config.go` (`MemoryConfig`, `ApplyEnv`)
- Modify: `internal/app/server.go` (retriever construction ~line 399; RouterConfig ~line 836)
- Test: `internal/config/config_test.go` (append)

- [ ] **Step 1: Write the failing config test**

Append to `internal/config/config_test.go`:

```go
func TestRetrievalTraceEnvOverride(t *testing.T) {
	t.Setenv("COGITATOR_RETRIEVAL_TRACE", "1")
	cfg := Default()
	cfg.ApplyEnv()
	if !cfg.Memory.RetrievalTrace {
		t.Error("Memory.RetrievalTrace = false, want true with env=1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/config/ -run TestRetrievalTraceEnvOverride
```

Expected: FAIL to compile (`Memory.RetrievalTrace` undefined).

- [ ] **Step 3: Add the config field + env override**

In `internal/config/config.go`, add to `MemoryConfig` (after `DedupSimilarityThreshold`):

```go
	RetrievalTrace bool `yaml:"retrieval_trace"`
```

In `ApplyEnv()`, alongside the other overrides, add:

```go
	if v := os.Getenv("COGITATOR_RETRIEVAL_TRACE"); v == "1" || strings.EqualFold(v, "true") {
		c.Memory.RetrievalTrace = true
	}
```

Ensure `"strings"` is imported in `config.go` (it likely is; if not, add it).

- [ ] **Step 4: Run config tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/config/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Wire stats + ring in app.New and pass to retriever + router**

In `internal/app/server.go`, just before the `retriever := memory.NewRetriever(memory.RetrieverConfig{` line (~399), create the instrumentation singletons:

```go
	retrievalStats := metrics.NewRetrievalStats(1000)
	retrievalTraces := memory.NewTraceRing(200)
```

Add these fields to the `memory.RetrieverConfig{...}` literal:

```go
		TraceEnabled: cfg.Memory.RetrievalTrace,
		TraceSink:    retrievalTraces,
		Stats:        retrievalStats,
```

In the `RouterConfig{...}` literal (~836, where `MetricsRing: metricsRing,` is set), add:

```go
		RetrievalStats:  retrievalStats,
		RetrievalTraces: retrievalTraces,
```

(`metrics` and `memory` are already imported in server.go.)

- [ ] **Step 6: Add the Router fields**

In `internal/api/router.go`, add to the `Router` struct (near `metricsRing *metrics.Ring`, ~line 109):

```go
	retrievalStats  *metrics.RetrievalStats
	retrievalTraces *memory.TraceRing
```

Add to `RouterConfig` (near `MetricsRing *metrics.Ring`, ~line 174):

```go
	RetrievalStats  *metrics.RetrievalStats
	RetrievalTraces *memory.TraceRing
```

In `NewRouter`, where `metricsRing: cfg.MetricsRing,` is assigned (~line 225), add:

```go
		retrievalStats:  cfg.RetrievalStats,
		retrievalTraces: cfg.RetrievalTraces,
```

Ensure `internal/memory` is imported in `router.go` (check; add if missing).

- [ ] **Step 7: Build all tags + run affected packages**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./... && go build -tags desktop ./... && go build -tags saas ./...
go test ./internal/config/ ./internal/app/ ./internal/api/ -count=1
```

Expected: builds succeed; tests pass.

- [ ] **Step 8: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/config/ server/internal/app/server.go server/internal/api/router.go
git commit -m "app: wire retrieval trace ring + stats into retriever and router"
```

---

### Task 7: Plant the session key in context for the trace

**Files:**
- Modify: `internal/agent/agent.go` (`Chat`, near top after signature)
- Test: covered indirectly; add a focused assertion in Task 10's chat test.

Design: the retriever reads `reqctx.SessionKey(ctx)`. The agent is the single chokepoint all chat paths flow through, so set it there once.

- [ ] **Step 1: Set the session key in `Chat`**

In `internal/agent/agent.go`, inside `Chat`, immediately after the function opens (before the `a.mu.RLock()` block is fine), add:

```go
	if req.SessionKey != "" {
		ctx = reqctx.WithSessionKey(ctx, req.SessionKey)
	}
```

Add the import `"github.com/cogitatorai/cogitator/server/internal/reqctx"` to `agent.go`.

- [ ] **Step 2: Build + test agent package**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./... && go test ./internal/agent/ -count=1
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/agent/agent.go
git commit -m "agent: propagate session key into context for retrieval trace"
```

---

### Task 8: Surface retrieval metrics in `/api/status`

**Files:**
- Modify: `internal/api/system.go` (`handleSystemStatus`)
- Test: `internal/api/system_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/api/system_test.go`:

```go
func TestStatusIncludesRetrievalMetrics(t *testing.T) {
	router := setupTestRouter(t)
	// Inject a stats aggregator with one sample so the section is present.
	router.retrievalStats = metricsNewRetrievalStatsForTest()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["retrieval"]; !ok {
		t.Errorf("retrieval section missing: %v", body)
	}
}
```

Add a tiny helper at the bottom of `system_test.go` (keeps the import local and explicit):

```go
func metricsNewRetrievalStatsForTest() *metrics.RetrievalStats {
	s := metrics.NewRetrievalStats(10)
	s.Record(0.8, 2, 0.5)
	return s
}
```

Ensure `system_test.go` imports `"github.com/cogitatorai/cogitator/server/internal/metrics"`.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestStatusIncludesRetrievalMetrics
```

Expected: FAIL (`retrieval section missing`).

- [ ] **Step 3: Implement**

In `internal/api/system.go`, in `handleSystemStatus`, where the `http` metrics are added (`if r.metricsRing != nil { status["http"] = r.metricsRing.Snapshot() }`), add directly after:

```go
	if r.retrievalStats != nil {
		status["retrieval"] = r.retrievalStats.Snapshot()
	}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestStatus -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/api/system.go server/internal/api/system_test.go
git commit -m "api: surface retrieval metrics in /api/status"
```

---

### Task 9: Admin endpoint `GET /api/admin/retrieval-traces`

**Files:**
- Create: `internal/api/retrieval_traces.go`
- Modify: `internal/api/router.go` (register route under `adminOnly`, ~line 284)
- Test: `internal/api/retrieval_traces_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/retrieval_traces_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/memory"
)

func TestRetrievalTracesRequiresAdmin(t *testing.T) {
	router := setupTestRouter(t)
	ring := memory.NewTraceRing(8)
	ring.Record(&memory.RetrievalTrace{RequestID: "r1", UserID: "u1", SessionKey: "web:default"})
	router.retrievalTraces = ring

	// Non-admin (or unauthenticated, depending on setupTestRouter defaults) is forbidden.
	req := httptest.NewRequest("GET", "/api/admin/retrieval-traces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 && w.Code != 401 {
		t.Fatalf("non-admin status = %d, want 401/403", w.Code)
	}
}

func TestRetrievalTracesHandlerReturnsRing(t *testing.T) {
	router := setupTestRouter(t)
	ring := memory.NewTraceRing(8)
	ring.Record(&memory.RetrievalTrace{RequestID: "r1", UserID: "u1", SessionKey: "web:default"})
	ring.Record(&memory.RetrievalTrace{RequestID: "r2", UserID: "u2", SessionKey: "web:other"})
	router.retrievalTraces = ring

	// Call the handler directly to bypass auth middleware and assert payload + filtering.
	req := httptest.NewRequest("GET", "/api/admin/retrieval-traces?session=web:default", nil)
	w := httptest.NewRecorder()
	router.handleRetrievalTraces(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d; body %s", w.Code, w.Body.String())
	}
	var body struct {
		Traces []memory.RetrievalTrace `json:"traces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Traces) != 1 || body.Traces[0].RequestID != "r1" {
		t.Errorf("filtered traces = %+v, want only r1", body.Traces)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestRetrievalTraces
```

Expected: FAIL (`handleRetrievalTraces` undefined; 404 route).

- [ ] **Step 3: Implement the handler**

Create `internal/api/retrieval_traces.go`:

```go
package api

import "net/http"

// handleRetrievalTraces returns the recent retrieval traces (newest first).
// Admin-gated at the router. The ring is empty unless COGITATOR_RETRIEVAL_TRACE
// is enabled; the response is still a valid empty list in that case. Optional
// ?session= and ?user= filters narrow the result.
func (r *Router) handleRetrievalTraces(w http.ResponseWriter, req *http.Request) {
	if r.retrievalTraces == nil {
		writeJSON(w, http.StatusOK, map[string]any{"traces": []any{}})
		return
	}
	sessionFilter := req.URL.Query().Get("session")
	userFilter := req.URL.Query().Get("user")

	all := r.retrievalTraces.Snapshot()
	traces := all[:0:0]
	for _, t := range all {
		if sessionFilter != "" && t.SessionKey != sessionFilter {
			continue
		}
		if userFilter != "" && t.UserID != userFilter {
			continue
		}
		traces = append(traces, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": traces})
}
```

- [ ] **Step 4: Register the route (admin only)**

In `internal/api/router.go`, in the `adminOnly := requireRole("admin")` block (~line 283), add:

```go
		r.mux.Handle("GET /api/admin/retrieval-traces", adminOnly(http.HandlerFunc(r.handleRetrievalTraces)))
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestRetrievalTraces -count=1
```

Expected: PASS. (If `setupTestRouter` authenticates as admin by default, the first test's 401/403 expectation may need to assert via a non-admin token — inspect `setupTestRouter` and adjust: the intent is "admin-gated".)

- [ ] **Step 6: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/api/retrieval_traces.go server/internal/api/retrieval_traces_test.go server/internal/api/router.go
git commit -m "api: add admin GET /api/admin/retrieval-traces endpoint"
```

---

### Task 10: `?debug=retrieval` inline trace on `/api/chat`

**Files:**
- Modify: `internal/api/chat.go` (`chatResponse`, `handleChat`)
- Test: `internal/api/chat_test.go` (append; create if absent — check with `ls internal/api/chat_test.go`)

Design: when an admin requests `POST /api/chat?debug=retrieval`, plant a `memory.TraceHolder` in the context via `memory.WithTrace`, pass that context to `agent.Chat`, and include the captured trace in the response. Non-admins are ignored.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/chat_test.go` (inspect existing chat tests first for the fake-agent/admin-auth helpers and mirror them):

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
ls internal/api/chat_test.go 2>/dev/null && grep -n "setupTestRouter\|agent\|admin\|handleChat" internal/api/chat_test.go | head
```

Then add a test that drives `handleChat` with `?debug=retrieval` as an admin and asserts the response carries a `retrieval_trace` object. The exact wiring depends on how `setupTestRouter` provides an agent + retriever; if the test harness cannot run a full chat turn, assert instead that the request context handed to the agent carries a trace holder (use a stub agent that calls `memory.WithTrace`'s holder via the context). At minimum assert:

```go
func TestChatDebugReturnsTraceForAdmin(t *testing.T) {
	// Build a router whose agent, when Chat is called, populates the trace holder
	// found in the context (simulating the retriever). Mirror the existing
	// chat-handler test setup. Then POST /api/chat?debug=retrieval as admin and
	// assert chatResponse.retrieval_trace is present; POST without ?debug and
	// assert it is omitted.
}
```

> If `chat_test.go` does not exist or lacks an agent stub, create the minimal stub needed (a fake implementing the `r.agent` interface used by `handleChat`) and keep it in the test file.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestChatDebug
```

Expected: FAIL (no `retrieval_trace` field).

- [ ] **Step 3: Add the response field and debug wiring**

In `internal/api/chat.go`, extend `chatResponse`:

```go
type chatResponse struct {
	Content        string                  `json:"content"`
	SessionKey     string                  `json:"session_key"`
	ToolsUsed      any                     `json:"tools_used,omitempty"`
	RetrievalTrace *memory.RetrievalTrace `json:"retrieval_trace,omitempty"`
}
```

Add the import `"github.com/cogitatorai/cogitator/server/internal/memory"`.

In `handleChat`, after `userRole` is resolved and before the `r.agent.Chat(...)` call, plant the holder when an admin asks for it:

```go
	ctx := req.Context()
	var traceHolder *memory.TraceHolder
	if userRole == "admin" && req.URL.Query().Get("debug") == "retrieval" {
		ctx, traceHolder = memory.WithTrace(ctx)
	}
```

Change the `r.agent.Chat(req.Context(), ...)` call to use `ctx`:

```go
	resp, err := r.agent.Chat(ctx, agent.ChatRequest{
```

In the success response, attach the trace:

```go
	out := chatResponse{
		Content:    resp.Content,
		SessionKey: body.SessionKey,
		ToolsUsed:  resp.ToolsUsed,
	}
	if traceHolder != nil {
		out.RetrievalTrace = traceHolder.Get()
	}
	writeJSON(w, http.StatusOK, out)
```

(Replace the existing `writeJSON(w, http.StatusOK, chatResponse{...})` block accordingly.)

- [ ] **Step 4: Run tests**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./internal/api/ -run TestChat -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/internal/api/chat.go server/internal/api/chat_test.go
git commit -m "api: return inline retrieval trace on /api/chat?debug=retrieval for admins"
```

---

### Task 11: Final verification, docs, review

**Files:**
- Modify: `IMPROVEMENTS.md` (checkboxes + status note) — local working-tree doc, not committed (untracked by project convention).

- [ ] **Step 1: Full build (all tags), vet, race, lint**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go vet ./... && go build ./... && go build -tags desktop ./... && go build -tags saas ./...
go test -race -count=1 ./...
make lint 2>/dev/null || true
```

Expected: everything passes. The trace ring and stats are touched by concurrent retrievals; `-race` must be clean (all shared state is mutex-guarded).

- [ ] **Step 2: Manual smoke (optional but recommended)**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
# With a configured provider+embedder and an admin token, enable tracing and hit chat.
# COGITATOR_RETRIEVAL_TRACE=1 ./cogitator  (separate shell)
# curl -s -H "Authorization: Bearer <admin>" "localhost:8484/api/status" | jq .retrieval
# curl -s -H "Authorization: Bearer <admin>" -X POST "localhost:8484/api/chat?debug=retrieval" \
#   -d '{"message":"hi"}' | jq .retrieval_trace
# curl -s -H "Authorization: Bearer <admin>" "localhost:8484/api/admin/retrieval-traces" | jq '.traces | length'
```

(Do not start services yourself; ask the user to run these if a live check is wanted.)

- [ ] **Step 3: Update IMPROVEMENTS.md**

Tick in §5.3: the "Per-turn retrieval trace" bullet and the "Retrieval metrics" bullet. Note in §1.2 that the retrieval metrics now feed `/api/status`. Append a status note near the top:

> Update (2026-06-15): retrieval trace + metrics landed on `feat/retrieval-trace`: config-gated metadata trace ring (`COGITATOR_RETRIEVAL_TRACE`), admin `GET /api/admin/retrieval-traces`, `?debug=retrieval` inline on `/api/chat`, always-on retrieval metrics in `/api/status`, request-ID correlation via new `internal/reqctx`.

- [ ] **Step 4: Code review**

Run a review over the branch diff (`git diff main...HEAD`) using the `code-review` skill (the `code-reviewer` agent is not registered in this environment). Address findings, then use superpowers:finishing-a-development-branch to decide merge/PR (a PR to `main` matching the established flow is expected; do not push or open a PR without the user's say-so unless already authorized).

---

## Self-review notes

- **Spec coverage:** trace data model (Task 3), record-at-source + reqctx (Tasks 1,3,4,5,7), config gating (Task 6), trace ring + admin API (Tasks 3,9), `?debug=retrieval` inline (Task 10), always-on aggregate metrics in `/api/status` (Tasks 2,6,8), logging under request ID (Tasks 4,5). Out-of-scope items (dashboard widget, DB persistence, threshold tuning, §5.2 eval baselines) are not included, per the spec.
- **Type consistency:** `TraceSink.Record(*RetrievalTrace)` and `StatsSink.Record(topSim, injected, budgetUtil)` are defined in Task 3 and implemented by `*TraceRing` (Task 3) and `*metrics.RetrievalStats` (Task 2); `metrics.RetrievalStats.Record` signature matches `StatsSink`. `RetrieverConfig.{TraceEnabled,TraceSink,Stats}` (Task 4) match the app wiring (Task 6). `RetrievalTrace`/`TraceCandidate` JSON fields are stable across the admin endpoint (Task 9) and the chat inline response (Task 10). Drop-reason constants (`DropBelowMinSimilarity`, `DropTokenBudget`, `DropOutsideTopK`, `DropLLMNotSelected`) are defined once in Task 3 and used in Tasks 4/5.
- **Known verify-during-execution points (called out inline):** real store/embedder/provider test-helper names in Tasks 4/5 (copy from `retriever_test.go`); whether `setupTestRouter` authenticates as admin by default (Task 9 first test); the `summaries` variable name and element fields in `retrieveLLM` (Task 5). These are confirmations, not redesigns.
