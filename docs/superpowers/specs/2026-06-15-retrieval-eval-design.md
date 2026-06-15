# Trustworthy vector-path retrieval eval (offline, manual, CI-ready) — Design

**Date:** 2026-06-15
**Scope:** First sub-project of the agent-quality evaluation strategy (IMPROVEMENTS.md §5.2 / §5.3). Makes the existing `server/eval` harness measure the **real production retrieval path** against a realistic embedded fixture graph, reproducibly, and surfaces *why* expected memories were or weren't retrieved.

## Evaluation strategy (context)

Evaluating "the agent on memory retrieval" spans a pipeline — `enrichment → storage/embedding → retrieval → injection → generation` — and the reported "it ignored my memory" failure can live in any stage. The agreed strategy is a four-layer pyramid:

- **L1 — Retrieval mechanics** (deterministic, no LLM/API): does `retrieveVector` threshold/rank/budget-fill/drop correctly? Hermetic, always runnable.
- **L2 — Retrieval quality** (real embedder, cached vectors): does the real embedding model actually return the right memories over a realistic graph?
- **L3 — End-to-end behavioral** (real agent + LLM): does the reply *use* the retrieved memory? (later sub-project)
- **L4 — Production signals**: the live app's `/api/status` retrieval metrics + the trace shipped in PR #39 (already done; not part of the offline harness).

**This sub-project delivers L1 + L2 for the retrieval stage**, plus the dataset, faithful seeding, trace-powered diagnostics, and the offline/CI-ready run modes they depend on. The regression gate (L?/baselines), L3, threshold tuning, CI workflow wiring, and the enrichment/reflection stages are explicitly out of scope (later sub-projects).

## Problem with the current harness

- **The entire `server/eval/` directory is gitignored** (`.gitignore:52`), so the harness is local-only — it cannot run in CI and fixtures/caches cannot be shared or reproduced.
- **The retrieval stage never exercises the production vector path.** Fixtures carry an `embedding` field that is never loaded; `runRetrieval` constructs the retriever with **no embedder** (`runner.go:248-253`), so it falls back to LLM classification (`retrieveLLM`). Production users with an embedder hit `retrieveVector` (cosine similarity, `MinSimilarity` 0.3, `TypeBoost`, token-budget fill) — the mechanics that actually explain the "ignored my memory" reports — and none of it is tested.
- **Dataset is too small** (3 query cases, 4 fixture nodes) to detect drift.
- **No insight into *why*** a memory was missed (recall is just a number).

## Goals / non-goals

**Goals**
1. Track the harness in git (narrow the ignore rule) so it is reproducible and CI-capable.
2. Exercise the real `retrieveVector` path with faithful seeding and faithful query construction.
3. Two embedders: a hermetic deterministic embedder (L1) and a real-but-cached embedder (L2) with graceful degradation.
4. A realistic fixture graph and a query case set covering the known failure modes.
5. Trace-powered diagnostics: for every expected-but-missed node, report the drop reason.
6. Offline mode + exit codes + a `go test` wrapper so the pipeline runs in CI with no secrets.

**Non-goals (later sub-projects)**
- Baselines + threshold regression gate; CI workflow wiring.
- L3 end-to-end "did the reply use the memory."
- Threshold-tuning sweeps.
- Reworking the enrichment/reflection stages (they keep working; only retrieval changes).

## Architecture

### 0. Track the harness (precondition)

Replace the `.gitignore` line `server/eval/` with narrow rules so the harness, fixtures, and committed embedding cache are tracked while the ephemeral bits stay ignored:

```
server/eval/results/            # per-run output snapshots — ephemeral
server/eval/testdata/cache/     # raw LLM response cache — ephemeral, may contain prompts
```

Then `git add` the harness code, README, and `testdata/{enrichment,reflection,retrieval}/`. A secret scan (2026-06-15) confirmed the tracked set contains no keys/credentials.

### 1. Two embedders behind `provider.Embedder`

Both implement `Embed(ctx, texts []string, model string) ([][]float32, error)`.

- **`deterministicEmbedder`** (L1, hermetic core): maps text → a fixed-dimension unit vector deterministically (hashed token-bag: lowercase-tokenize, hash each token into one of N buckets, L2-normalize). No API, no network, identical everywhere. Similarity is synthetic but **predictable**, so mechanics cases can assert known rankings. Selected with `-embedder=deterministic`.
- **`realCachedEmbedder`** (L2, fidelity): wraps a real `provider.Embedder` with a **committed** on-disk cache. Cache key = `(model, sha256(text))`; entries stored under `server/eval/testdata/embeddings/<model>/<sha>.json` (committed, distinct from the gitignored response `cache/`). On hit → return cached vector (offline, free). On miss → if a live API key is available and not in offline mode, call the real embedder and write the cache file; otherwise it is a cache-miss error (offline) or a skip (see run modes). The pinned model name is recorded in the result. Selected with `-embedder=real -model <m>` (default model from `config.Default().Memory.EmbeddingModel`, i.e. `text-embedding-3-small`).

### 2. Faithful seeding & retrieval

- **Seeding:** reuse the production `memory.NodeEmbedder` (which builds embedding text as `Title + Summary + Tags + RetrievalTriggers + truncated content`, `embedder.go:83`) so fixtures are embedded with the exact production recipe, then persisted via `store.SaveEmbedding`. Fixtures carry `user_id`, `pinned`, and `edges` so the eval reconstructs a real multi-user graph including pinned-bypass and 1-hop connected nodes.
- **Retrieval:** construct `memory.NewRetriever` **with the chosen embedder** and production defaults read from `config.Default()` (`MinSimilarity` 0.3, `TypeBoost` 1.1, `TokenBudget` 2000, `ContextWindow` 5, `TopK` 5), all recorded in the result so a run is self-describing. Invoke `retriever.Retrieve(ctx, userID, message, history)` so query construction (`buildRetrievalText`: last-N history + current message) matches production exactly.

### 3. Trace-powered drop diagnostics

Run retrieval with a trace requested (via `memory.WithTrace(ctx)`, the holder shipped in PR #39). For every node in a case's `expected_node_ids` that was **not** injected, read the trace and attach the drop reason + score: `below_min_similarity` (with the actual cosine), `token_budget`, or `outside_top_k`. Surfaced per-case in the JSON/table output. This turns "recall 0.6" into "`node_coffee` missed — similarity 0.27 < 0.30 floor."

> Dependency: the `RetrievalTrace`/`WithTrace` types come from PR #39 (`feat/retrieval-trace`). This sub-project lands after #39 merges (or rebases onto it). The core L1/L2 eval works without the trace; diagnostics are the enhancement that needs it.

### 4. Data model changes

```go
// RetrievalCase gains optional history (to test dilution) and user scoping.
type RetrievalCase struct {
	ID             string    `json:"id"`
	Query          string    `json:"query"`
	History        []Message `json:"history,omitempty"`     // prior turns, oldest first
	UserID         string    `json:"user_id,omitempty"`     // which user's graph; "" = default seeded user
	ExpectedIDs    []string  `json:"expected_node_ids"`
	ExpectedNotIDs []string  `json:"expected_not_ids"`
	MinPrecision   float64   `json:"min_precision"`
	MinRecall      float64   `json:"min_recall"`
}

// RetrievalFixture gains graph shape. Embeddings are computed at seed time
// (not stored in the fixture), so the file stays small and model-agnostic.
type RetrievalFixture struct {
	ID                string   `json:"id"`
	UserID            string   `json:"user_id,omitempty"`
	Type              string   `json:"type"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	Tags              []string `json:"tags"`
	RetrievalTriggers []string `json:"retrieval_triggers,omitempty"`
	Content           string   `json:"content"`
	Pinned            bool     `json:"pinned,omitempty"`
	Edges             []FixtureEdge `json:"edges,omitempty"` // {target, weight}
}
```

The existing `embedding []float32` field on the fixture is removed (vectors now come from the embedder + committed cache, keyed by the embedded text). Per-case `Scores` gain `zero_retrieval` (1 if nothing was injected) and `expected_rank` (rank of the first expected node, 0 if absent); existing precision/recall/MRR/exclusion stay. `StageResult`/`CaseResult` gain an optional `diagnostics` field carrying the per-missed-node drop reasons.

### 5. Dataset

A realistic graph in `testdata/retrieval/`, sized for a faithful **per-user** noise ratio (retrieval filters by `userID`, so only one user's nodes compete per query):

- **~150 nodes total**, concentrated **~120 under one "heavy" user** + **~30 under a second user** (the second user exists for isolation/scoping tests — confirming one user's query never retrieves another's nodes). This gives ~120 competing candidates per heavy-user query.
- Of the heavy user's nodes, **~40 are curated**: the query targets plus deliberate adversarial distractors (near-duplicates such as a current vs. a stale "coffee preference," topically-adjacent facts) that probe each failure mode. The remaining **~110 are generated filler** — plausible, topically-diverse background memories that add density without needing hand-curation (generated once, e.g. LLM-assisted, then committed).
- Spread across `fact/preference/pattern/skill`, with several pinned nodes and edges between related nodes.
- The graph is **trivially extensible**: adding fixtures + re-running the cache-fill grows it to 300+ later without re-architecting. ~15–25 **semantic** query cases (L2) in `testdata/retrieval/cases.json` covering: direct match, paraphrase, multi-hop/connected, distractor rejection, history-dilution (long off-topic history), pinned-starvation, and genuine zero-retrieval. Plus ~6–10 **mechanics** cases (L1) in a separate `testdata/retrieval/mechanics.json` authored against the deterministic embedder where the expected ranking is known by construction, asserting: below-threshold drop, token-budget eviction, top-K cutoff, `TypeBoost` ordering, pinned bypass, zero-retrieval. The runner selects fixtures+cases by embedder mode: `-embedder=deterministic` loads `mechanics_fixtures.json` + `mechanics.json` (a small dedicated graph whose token overlap makes the deterministic embedder's similarity computable by hand); `-embedder=real` loads `fixtures.json` + `cases.json` (the realistic semantic graph). Both case files share the same `RetrievalCase` schema and both fixture files share the same `RetrievalFixture` schema.

### 6. Run modes & CI-readiness

- `eval run -stage retrieval -embedder deterministic` — hermetic, runs the L1 mechanics cases. No API, no cache, no network. Always available.
- `eval run -stage retrieval -embedder real -model <m>` — L2 semantic cases against the committed cache; on cache miss, calls the API if a key is present, else errors (see offline).
- `-offline` flag (and auto-on when no API key is configured): forbids live embedding calls. L1 always runs; L2 runs cache-only and a cache miss is a hard error naming the offending case ("run locally with an API key to regenerate and commit the embedding cache"). This keeps the committed cache complete and makes CI deterministic and secret-free.
- **Exit code** is non-zero when any case scores below its `MinPrecision`/`MinRecall` (per-case gate; the aggregate baseline gate is a later sub-project).
- **`go test` wrapper** (`eval/retrieval_eval_test.go`): runs the L1 deterministic cases and asserts all pass, so the hermetic layer rides the existing `go test ./...` step with zero new CI infra and zero secrets. A sibling test runs L2 in offline cache-only mode but **skips** (`t.Skip`) when the embedding cache is absent, so it never spuriously blocks a checkout that lacks the cache.

### 7. Error handling

- Missing embedder/model in real mode without cache or key → clear error, non-zero exit (not a panic).
- A fixture referencing an edge target that doesn't exist → fail fast with the fixture ID.
- Trace unavailable (diagnostics disabled) → metrics still computed; diagnostics simply omitted.
- Deterministic embedder is pure and total (never errors).

## Testing

- Unit: `deterministicEmbedder` determinism + dimensionality + normalization; `realCachedEmbedder` hit/miss/offline-error/write-on-miss (with a stub provider embedder); cache key stability across runs.
- Unit: faithful-seeding helper produces the same embedding text as `NodeEmbedder` for a node (guard against drift).
- The `go test` wrappers above (L1 always; L2 skip-when-no-cache).
- Scorer additions (`zero_retrieval`, `expected_rank`) with table cases.
- All green under `go test ./...`; the harness builds as part of `go build ./...` once tracked.

## Out of scope (explicit)

Baselines + threshold regression gate; CI workflow file; L3 end-to-end stage; threshold-tuning sweeps; LLM-as-judge; enrichment/reflection stage changes; any change to production `internal/memory` behavior (the eval only *observes* it). Note: this sub-project depends on PR #39 (trace types) for the diagnostics in §3.

## N-1 / multi-environment notes

The harness is a standalone CLI compiled separately from the server (`eval/cmd`), with its own use of `internal/*` packages. It runs against an in-memory SQLite store and never touches a running app or production data. No production code paths change. Tracking the directory is additive to the repo; the only behavioral change to the build is that `go build ./...` / `go test ./...` now include `server/eval` (it must therefore compile and pass — which it already does locally).
