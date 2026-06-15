# Cogitator Eval Harness

Evaluates LLM behavior across Cogitator's memory pipeline: enrichment, retrieval, and reflection. Scores outputs against golden datasets using deterministic metrics. Supports model comparison and response caching to minimize token costs.

## Quick Start

From `cogitator/server/eval/`:

```bash
# Run all stages against your configured provider
go run ./cmd/ run -provider openai -model gpt-4o

# Run a single stage
go run ./cmd/ run -provider openai -model gpt-4o -stage enrichment

# Retrieval mechanics, hermetic (no API key, no network — CI-safe):
go run ./cmd/ run -stage retrieval -embedder deterministic

# Retrieval semantic quality, real embeddings (first run embeds + commits the
# cache; reruns are free). -offline forbids live calls and uses the cache only:
go run ./cmd/ run -stage retrieval -embedder real -provider openai -model gpt-4o
go run ./cmd/ run -stage retrieval -embedder real -offline -provider openai -model gpt-4o

# Save results for comparison
go run ./cmd/ run -provider openai -model gpt-4o -out results/gpt4o.json
go run ./cmd/ run -provider anthropic -model claude-sonnet-4-20250514 -out results/sonnet.json

# Compare models side-by-side
go run ./cmd/ compare results/gpt4o.json results/sonnet.json

# Force fresh LLM calls (skip cache)
go run ./cmd/ run -provider openai -model gpt-4o -no-cache

# Clear cached responses
go run ./cmd/ cache clear
```

API keys are loaded from the OS keychain (same as the server), then `~/.cogitator/secrets.yaml`, then the `COGITATOR_API_KEY` environment variable. A key is **only** needed for LLM stages (enrichment) and `-embedder real` without `-offline`; the deterministic retrieval path and offline cached runs need no key.

## What It Evaluates

### Enrichment

Given a memory node (title + content), does the LLM produce correct metadata?

| Metric | Description |
|--------|-------------|
| Type accuracy | Exact match on node_type (fact/preference/pattern) |
| Tag overlap | Jaccard similarity between predicted and expected tags |
| Summary quality | Keyword inclusion/exclusion checks |

### Retrieval

Given a query and a set of pre-seeded memories, does the retriever surface the right nodes? Retrieval exercises the **real production vector path** (`memory.Retriever.Retrieve` with the same `MinSimilarity`/`TypeBoost`/token-budget defaults), seeding a temp SQLite DB with fixtures embedded via the production node-embedding recipe. No production data is touched.

| Metric | Description |
|--------|-------------|
| Precision@K | Fraction of returned nodes that are relevant |
| Recall@K | Fraction of expected nodes that were returned |
| MRR | Reciprocal rank of the first relevant result |
| Exclusion | Verifies `expected_not_ids` nodes are not returned (gates pass/fail) |
| Zero-retrieval | 1 when nothing was injected (catches over-strict thresholds) |
| Expected rank | Rank of the first expected node in the result |

A run with `min_precision`/`min_recall` thresholds **passes** only when `precision ≥ min && recall ≥ min && exclusion == 1.0`. For every expected-but-missed node, the per-case output includes a **drop-reason diagnostic** (`below_min_similarity` with the actual score, `token_budget`, `outside_top_k`, or `not_a_candidate`) — so a low recall tells you *why*, not just that.

Two embedder modes:

- **`-embedder deterministic` (L1, hermetic):** a built-in token-bag embedder, no API/network. Runs the `mechanics.json` cases (known rankings by construction) to lock the threshold/budget/drop/TypeBoost mechanics. This is the always-on, CI-safe gate.
- **`-embedder real` (L2, semantic):** a real embedding model (default `text-embedding-3-small`) with a **committed vector cache** under `testdata/embeddings/<model>/`. Runs the `cases.json` semantic graph (~150 nodes, multiple users, near-duplicate distractors). First run embeds + commits the cache; later runs (incl. `-offline`) reproduce from it with no network. Catches embedding-model and threshold-tuning drift.

### Reflection

Given a conversation snippet, does the signal detector identify the correct behavioral signal?

| Metric | Description |
|--------|-------------|
| Signal accuracy | Correct signal type detected (correction/refinement/acknowledgment) |
| Confidence met | Detected confidence meets the expected minimum |
| False positive | Signal detected when none was expected |

Reflection uses pattern matching (no LLM call) for English messages.

## Golden Datasets

Test cases live in `testdata/` as JSON files:

```
testdata/
  enrichment/cases.json              # enrichment test cases
  retrieval/cases.json               # L2 semantic retrieval queries (real embedder)
  retrieval/fixtures.json            # L2 ~150-node graph (multi-user, distractors)
  retrieval/mechanics.json           # L1 mechanics cases (deterministic embedder)
  retrieval/mechanics_fixtures.json  # L1 hand-built graph with known rankings
  retrieval/cache/                   # raw LLM response cache (gitignored)
  embeddings/<model>/                # committed vector cache for L2 (one file per text)
  reflection/cases.json              # conversation snippets with expected signals
```

Add more test cases by editing the JSON files. For L2 (real embedder), after adding fixtures/queries, run `-embedder real` once locally (with an API key) to embed and commit the new vectors; reruns are then free/offline. CI runs L1 (hermetic) plus L2 against the committed cache.

## Response Caching

LLM responses are cached on disk by hashing the prompt + provider + model. The second run with the same model is free (no API calls). Cache files are stored in `testdata/cache/` (gitignored).

Use `-no-cache` to force fresh calls. Use `cache clear` to wipe the cache directory.

Retrieval embeddings use a **separate, committed** cache under `testdata/embeddings/<model>/` (one JSON vector per text, keyed by `sha256(text)`). Unlike the response cache, it is checked into git so L2 retrieval evals reproduce offline in CI without an API key. Regenerate it by running `-embedder real` locally after changing fixtures/queries.

## Architecture

```
eval/
  types.go                    # test case and result structs
  cache.go                    # SHA-256 keyed response cache
  embedder_deterministic.go   # hermetic token-bag embedder (L1)
  embedder_cached.go          # real embedder + committed vector cache (L2)
  scorer.go                   # Jaccard, precision/recall, MRR, zero-retrieval, rank
  runner.go                   # stage execution (enrichment, retrieval, reflection)
  reporter.go                 # table, JSON, and comparison output
  retrieval_eval_test.go      # CI go-test wrappers (L1 hermetic, L2 offline-cache)
  cmd/main.go                 # CLI entry point
  testdata/                   # golden datasets + committed embedding cache
```

The runner calls the same prompt-building functions the server uses (`BuildEnrichmentPrompt`, `BuildReflectionPrompt`, `DetectSignals`), so the eval tests real prompts, not approximations.

## Next Steps

- **Baselines + regression gate.** Persist results per version and diff new runs against a committed baseline with per-metric thresholds, exiting non-zero on regression. (Per-case threshold gating + non-zero exit already exist; the aggregate baseline comparison is the next piece.)
- **End-to-end agent stage.** Seed memories, run a real `agent.Chat` turn, and assert the reply actually uses the retrieved memory (keyword rubric, later LLM-judge). This is the only layer that catches "retrieved but ignored."
- **Evidence-based threshold tuning.** With the L2 graph in place, sweep `MinSimilarity`/`ContextWindow`/`TypeBoost` against the dataset as a one-command experiment. (The known-hard temporal-distractor case — current vs. stale preference — is a documented motivator: pure vector similarity has no recency signal.)
- **Expand the datasets.** The L2 graph is ~150 nodes; grow it toward 300+ and convert every user-reported "it ignored my memory" into a case. Also: enrichment trigger-coverage metric, non-English reflection cases.
- **LLM-as-judge layer.** For subjective quality (summary coherence, reply personalization) that deterministic metrics cannot capture, add an optional grading-LLM pass.
