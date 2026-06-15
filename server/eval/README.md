# Cogitator Eval Harness

Evaluates LLM behavior across Cogitator's memory pipeline: enrichment, retrieval, and reflection. Scores outputs against golden datasets using deterministic metrics. Supports model comparison and response caching to minimize token costs.

## Quick Start

From `cogitator/server/eval/`:

```bash
# Run all stages against your configured provider
go run ./cmd/ run -provider openai -model gpt-4o

# Run a single stage
go run ./cmd/ run -provider openai -model gpt-4o -stage enrichment

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

API keys are loaded from the OS keychain (same as the server), then `~/.cogitator/secrets.yaml`, then the `COGITATOR_API_KEY` environment variable.

## What It Evaluates

### Enrichment

Given a memory node (title + content), does the LLM produce correct metadata?

| Metric | Description |
|--------|-------------|
| Type accuracy | Exact match on node_type (fact/preference/pattern) |
| Tag overlap | Jaccard similarity between predicted and expected tags |
| Summary quality | Keyword inclusion/exclusion checks |

### Retrieval

Given a query and a set of pre-seeded memories, does the retriever surface the right nodes?

| Metric | Description |
|--------|-------------|
| Precision@K | Fraction of returned nodes that are relevant |
| Recall@K | Fraction of expected nodes that were returned |
| MRR | Reciprocal rank of the first relevant result |
| Exclusion | Verifies irrelevant nodes are not returned |

Retrieval tests use an in-memory SQLite database seeded with fixtures. No production data is touched.

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
  enrichment/cases.json       # enrichment test cases
  retrieval/cases.json        # retrieval queries with expected results
  retrieval/fixtures.json     # pre-seeded nodes for retrieval tests
  reflection/cases.json       # conversation snippets with expected signals
```

Add more test cases by editing the JSON files. No code changes needed. The eval harness picks them up automatically.

## Response Caching

LLM responses are cached on disk by hashing the prompt + provider + model. The second run with the same model is free (no API calls). Cache files are stored in `testdata/cache/` (gitignored).

Use `-no-cache` to force fresh calls. Use `cache clear` to wipe the cache directory.

## Architecture

```
eval/
  types.go       # test case and result structs
  cache.go       # SHA-256 keyed response cache
  scorer.go      # Jaccard, precision/recall, MRR
  runner.go      # stage execution (enrichment, retrieval, reflection)
  reporter.go    # table, JSON, and comparison output
  cmd/main.go    # CLI entry point
  testdata/      # golden datasets
```

The runner calls the same prompt-building functions the server uses (`BuildEnrichmentPrompt`, `BuildReflectionPrompt`, `DetectSignals`), so the eval tests real prompts, not approximations.

## Next Steps

- **Expand golden datasets.** 5 enrichment, 3 retrieval, and 4 reflection cases is a starting point. Add cases for edge cases: multilingual content, long documents, ambiguous queries, contradictory memories.
- **Add trigger coverage metric.** The enrichment prompt generates retrieval triggers, but they are not scored yet. Add expected triggers to enrichment cases and compute coverage.
- **Add non-English reflection cases.** The current reflection tests only exercise the pattern-matching path (English). Add non-English cases to test the LLM fallback via `BuildReflectionPrompt`.
- **CI integration.** Add `go test ./eval/` to CI. Use cached responses so CI runs are deterministic and free. Failures indicate regressions in prompt construction or scoring logic.
- **Trend tracking.** Save JSON results over time and build a simple dashboard (or script) to track metric trends across releases and model updates.
- **LLM-as-judge layer.** For subjective quality dimensions (summary coherence, trigger relevance) that deterministic metrics cannot capture, add an optional second-pass evaluation using a grading LLM.
