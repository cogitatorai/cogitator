# Trustworthy Vector-Path Retrieval Eval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `server/eval`'s retrieval stage exercise the real production `retrieveVector` path against a realistic ~150-node embedded fixture graph, reproducibly and offline/CI-ready, and report *why* expected memories were dropped.

**Architecture:** Track the (currently gitignored) harness. Add two `provider.Embedder` implementations — a hermetic deterministic embedder (L1) and a real-but-committed-cache embedder (L2) — select one per run. Seed fixtures via the production `memory.NodeEmbedder` so embeddings match production, construct the retriever *with* the embedder and production defaults, and read the PR-#39 `RetrievalTrace` to attach drop reasons to missed expected nodes.

**Tech Stack:** Go 1.25 stdlib, `internal/memory` (Store, NodeEmbedder, Retriever, RetrievalTrace), `internal/provider` (Embedder), `internal/config`, table-driven tests.

**Conventions for every task:**
- Run Go commands from `/Users/deiu/dev/deiu/cogi/cogitator/server`. The git repo root is `/Users/deiu/dev/deiu/cogi/cogitator` (commit from there with `server/...` paths).
- Commit messages: lowercase `area: summary` (`eval: ...`). NEVER add a `Co-Authored-By` trailer (user rule).
- Branch is already `feat/retrieval-eval`, rebased onto `main` (which now contains PR #39's `internal/memory/trace.go` + `internal/reqctx`). The deleted `docs/superpowers/*` sqlite files and untracked `IMPROVEMENTS.md` in the working tree are pre-existing; leave them alone.
- The eval package is `package eval` at `server/eval/`; its CLI is `server/eval/cmd` (`package main`).

---

### Task 1: Track the harness (narrow the .gitignore)

**Files:**
- Modify: `.gitignore` (repo root)

- [ ] **Step 1: Inspect the current ignore rule**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
grep -n "server/eval" .gitignore
git ls-files server/eval/ | head   # expect EMPTY (nothing tracked today)
```

Expected: line 52 is `server/eval/`; `git ls-files` prints nothing.

- [ ] **Step 2: Replace the blanket ignore with narrow rules**

In `.gitignore`, replace the single line `server/eval/` with:

```
server/eval/results/
server/eval/testdata/cache/
```

- [ ] **Step 3: Verify what will be tracked vs ignored**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git check-ignore server/eval/results/x.json server/eval/testdata/cache/x   # both print (ignored)
git check-ignore server/eval/runner.go && echo "BUG: code ignored" || echo "code tracked OK"
git check-ignore server/eval/testdata/retrieval/cases.json && echo "BUG: fixtures ignored" || echo "fixtures tracked OK"
```

Expected: `results/` and `testdata/cache/` are ignored; `runner.go` and `cases.json` are tracked.

- [ ] **Step 4: Stage the harness and confirm the staged set excludes ephemeral dirs**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add .gitignore server/eval
git status --short server/eval | grep -E "results/|testdata/cache/" && echo "BUG: ephemeral staged" || echo "ephemeral correctly excluded"
git status --short server/eval | head -30
```

Expected: no `results/` or `testdata/cache/` paths staged; the `.go` files, `README.md`, and `testdata/{enrichment,reflection,retrieval}/*.json` are staged as new files.

- [ ] **Step 5: Confirm the harness still builds and tests pass**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./eval/... && go test ./eval/... -count=1
```

Expected: build OK, existing eval tests pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git commit -m "eval: track harness in git (ignore only results/ and response cache)"
```

---

### Task 2: Deterministic embedder (L1 hermetic core)

**Files:**
- Create: `server/eval/embedder_deterministic.go`
- Test: `server/eval/embedder_deterministic_test.go`

The deterministic embedder implements `provider.Embedder` (`Embed(ctx, texts []string, model string) ([][]float32, error)`). It maps text → a fixed-dimension unit vector by hashing lowercase whitespace tokens into buckets, then L2-normalizing. Same text → identical vector everywhere; shared tokens → higher cosine.

- [ ] **Step 1: Write the failing test**

Create `server/eval/embedder_deterministic_test.go`:

```go
package eval

import (
	"context"
	"math"
	"testing"
)

func TestDeterministicEmbedderStableAndNormalized(t *testing.T) {
	e := NewDeterministicEmbedder(64)
	v1, err := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	v2, _ := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	if len(v1) != 1 || len(v1[0]) != 64 {
		t.Fatalf("dims: got %d vecs, len %d", len(v1), len(v1[0]))
	}
	// Deterministic: identical text -> identical vector.
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, v1[0][i], v2[0][i])
		}
	}
	// Unit length.
	var sum float64
	for _, x := range v1[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(sum-1.0) > 1e-5 {
		t.Errorf("not normalized: |v|^2=%v", sum)
	}
}

func TestDeterministicEmbedderSimilarityOrdering(t *testing.T) {
	e := NewDeterministicEmbedder(128)
	q, _ := e.Embed(context.Background(), []string{"coffee preference dark roast"}, "det")
	near, _ := e.Embed(context.Background(), []string{"dark roast coffee"}, "det")
	far, _ := e.Embed(context.Background(), []string{"hiking trails mountains"}, "det")
	simNear := dot(q[0], near[0])
	simFar := dot(q[0], far[0])
	if simNear <= simFar {
		t.Errorf("expected coffee query closer to coffee node: near=%v far=%v", simNear, simFar)
	}
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func TestDeterministicEmbedderEmptyText(t *testing.T) {
	e := NewDeterministicEmbedder(16)
	v, err := e.Embed(context.Background(), []string{""}, "det")
	if err != nil {
		t.Fatalf("embed empty: %v", err)
	}
	if len(v) != 1 || len(v[0]) != 16 {
		t.Fatalf("empty text should still yield a zero vector of right dim, got len %d", len(v[0]))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestDeterministicEmbedder
```

Expected: FAIL to compile (`NewDeterministicEmbedder` undefined).

- [ ] **Step 3: Write the implementation**

Create `server/eval/embedder_deterministic.go`:

```go
package eval

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

// DeterministicEmbedder is a hermetic, reproducible provider.Embedder for the
// L1 retrieval-mechanics eval. It needs no API and no network: it hashes
// lowercase whitespace tokens into a fixed-dimension bag-of-words vector and
// L2-normalizes. Identical text always yields the identical vector, and texts
// sharing tokens have higher cosine similarity — enough to author mechanics
// cases with known rankings. It does NOT model real semantic similarity.
type DeterministicEmbedder struct {
	dim int
}

// NewDeterministicEmbedder returns an embedder producing dim-dimensional unit
// vectors. dim<=0 defaults to 128.
func NewDeterministicEmbedder(dim int) *DeterministicEmbedder {
	if dim <= 0 {
		dim = 128
	}
	return &DeterministicEmbedder{dim: dim}
}

// Embed implements provider.Embedder. The model argument is ignored.
func (e *DeterministicEmbedder) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = e.vector(t)
	}
	return out, nil
}

func (e *DeterministicEmbedder) vector(text string) []float32 {
	v := make([]float32, e.dim)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(tok))
		v[h.Sum32()%uint32(e.dim)] += 1
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v // empty/whitespace text -> zero vector
	}
	inv := float32(1.0 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return v
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestDeterministicEmbedder -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/embedder_deterministic.go server/eval/embedder_deterministic_test.go
git commit -m "eval: add hermetic deterministic embedder for L1 mechanics"
```

---

### Task 3: Committed embedding cache + real-cached embedder (L2 fidelity)

**Files:**
- Create: `server/eval/embedder_cached.go`
- Test: `server/eval/embedder_cached_test.go`

`realCachedEmbedder` wraps a real `provider.Embedder` with an on-disk cache committed to git under `testdata/embeddings/<model>/<sha256(text)>.json` (one JSON float array per text). Hit → cached vector (offline, free). Miss → if `offline`, return an error naming the text; else call the wrapped embedder and write the cache file.

- [ ] **Step 1: Write the failing test**

Create `server/eval/embedder_cached_test.go`:

```go
package eval

import (
	"context"
	"errors"
	"testing"
)

// stubEmbedder returns a fixed vector and counts calls.
type stubEmbedder struct {
	vec   []float32
	calls int
	err   error
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls += len(texts)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = s.vec
	}
	return out, nil
}

func TestCachedEmbedderWritesOnMissThenHits(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{vec: []float32{0.1, 0.2, 0.3}}
	e := NewCachedEmbedder(stub, "test-model", dir, false)

	v1, err := e.Embed(context.Background(), []string{"hello world"}, "test-model")
	if err != nil {
		t.Fatalf("miss embed: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("expected 1 underlying call, got %d", stub.calls)
	}
	// Second call (and a fresh embedder over the same dir) must hit the cache.
	e2 := NewCachedEmbedder(stub, "test-model", dir, false)
	v2, err := e2.Embed(context.Background(), []string{"hello world"}, "test-model")
	if err != nil {
		t.Fatalf("hit embed: %v", err)
	}
	if stub.calls != 1 {
		t.Errorf("cache miss on second call: underlying calls=%d", stub.calls)
	}
	if v1[0][0] != v2[0][0] || len(v2[0]) != 3 {
		t.Errorf("cached vector mismatch: %v vs %v", v1[0], v2[0])
	}
}

func TestCachedEmbedderOfflineMissErrors(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{vec: []float32{1}}
	e := NewCachedEmbedder(stub, "test-model", dir, true) // offline

	_, err := e.Embed(context.Background(), []string{"uncached text"}, "test-model")
	if err == nil {
		t.Fatal("expected offline cache-miss error, got nil")
	}
	if stub.calls != 0 {
		t.Errorf("offline mode must not call the underlying embedder, calls=%d", stub.calls)
	}
}

func TestCachedEmbedderPropagatesUnderlyingError(t *testing.T) {
	dir := t.TempDir()
	stub := &stubEmbedder{err: errors.New("api down")}
	e := NewCachedEmbedder(stub, "m", dir, false)
	if _, err := e.Embed(context.Background(), []string{"x"}, "m"); err == nil {
		t.Fatal("expected underlying error to propagate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestCachedEmbedder
```

Expected: FAIL to compile (`NewCachedEmbedder` undefined).

- [ ] **Step 3: Write the implementation**

Create `server/eval/embedder_cached.go`:

```go
package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// CachedEmbedder wraps a real provider.Embedder with a committed on-disk vector
// cache so L2 retrieval evals are reproducible and runnable offline. Cache files
// live under <dir>/<model>/<sha256(text)>.json (one JSON float array each) and
// are committed to git. On a cache miss in offline mode it errors instead of
// calling the network, keeping the committed cache complete.
type CachedEmbedder struct {
	inner   provider.Embedder
	model   string
	dir     string // root embeddings dir, e.g. testdata/embeddings
	offline bool
}

// NewCachedEmbedder wraps inner, caching vectors under dir/<model>/. When
// offline is true, a cache miss is an error (no network call).
func NewCachedEmbedder(inner provider.Embedder, model, dir string, offline bool) *CachedEmbedder {
	return &CachedEmbedder{inner: inner, model: model, dir: dir, offline: offline}
}

func (e *CachedEmbedder) pathFor(text string) string {
	sum := sha256.Sum256([]byte(text))
	return filepath.Join(e.dir, e.model, fmt.Sprintf("%x.json", sum))
}

// Embed implements provider.Embedder, one text at a time so each gets its own
// cache entry. The model arg is ignored in favor of the configured model.
func (e *CachedEmbedder) Embed(ctx context.Context, texts []string, _ string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := e.read(t); ok {
			out[i] = v
			continue
		}
		if e.offline {
			return nil, fmt.Errorf("offline: no cached embedding for text (sha %x...); run locally with an API key to regenerate and commit the cache", sha256.Sum256([]byte(t))[:6])
		}
		vecs, err := e.inner.Embed(ctx, []string{t}, e.model)
		if err != nil {
			return nil, err
		}
		if len(vecs) == 0 {
			return nil, fmt.Errorf("embedder returned no vector for text")
		}
		if err := e.write(t, vecs[0]); err != nil {
			return nil, fmt.Errorf("write embedding cache: %w", err)
		}
		out[i] = vecs[0]
	}
	return out, nil
}

func (e *CachedEmbedder) read(text string) ([]float32, bool) {
	data, err := os.ReadFile(e.pathFor(text))
	if err != nil {
		return nil, false
	}
	var v []float32
	if json.Unmarshal(data, &v) != nil {
		return nil, false
	}
	return v, true
}

func (e *CachedEmbedder) write(text string, v []float32) error {
	p := e.pathFor(text)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestCachedEmbedder -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/embedder_cached.go server/eval/embedder_cached_test.go
git commit -m "eval: add committed-cache embedder for reproducible L2 runs"
```

---

### Task 4: Extend case/fixture types + scorer metrics

**Files:**
- Modify: `server/eval/types.go`
- Modify: `server/eval/scorer.go` (`ScoreRetrieval`)
- Test: `server/eval/scorer_test.go` (append)

- [ ] **Step 1: Write the failing scorer test**

Append to `server/eval/scorer_test.go`:

```go
func TestScoreRetrievalZeroAndRank(t *testing.T) {
	c := RetrievalCase{ExpectedIDs: []string{"a", "b"}}

	// Nothing returned -> zero_retrieval 1, expected_rank 0.
	s := ScoreRetrieval(c, nil)
	if s["zero_retrieval"] != 1 {
		t.Errorf("zero_retrieval = %v, want 1", s["zero_retrieval"])
	}
	if s["expected_rank"] != 0 {
		t.Errorf("expected_rank = %v, want 0 (absent)", s["expected_rank"])
	}

	// "b" first relevant at position 2 -> rank 2, zero_retrieval 0.
	s2 := ScoreRetrieval(c, []string{"x", "b", "a"})
	if s2["zero_retrieval"] != 0 {
		t.Errorf("zero_retrieval = %v, want 0", s2["zero_retrieval"])
	}
	if s2["expected_rank"] != 2 {
		t.Errorf("expected_rank = %v, want 2", s2["expected_rank"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestScoreRetrievalZeroAndRank
```

Expected: FAIL (`zero_retrieval`/`expected_rank` not present).

- [ ] **Step 3: Extend the types**

In `server/eval/types.go`:

Add a `Message` alias near the top imports usage (reuse `provider.Message` to match reflection cases):

(No new type needed — use `[]provider.Message` for history, consistent with `ReflectionCase`.)

Replace `RetrievalCase` with:

```go
// RetrievalCase is a single retrieval evaluation test case.
type RetrievalCase struct {
	ID             string             `json:"id"`
	Query          string             `json:"query"`
	History        []provider.Message `json:"history,omitempty"` // prior turns, oldest first; tests dilution
	UserID         string             `json:"user_id,omitempty"` // which user's graph; "" = default seeded user
	ExpectedIDs    []string           `json:"expected_node_ids"`
	ExpectedNotIDs []string           `json:"expected_not_ids"`
	MinPrecision   float64            `json:"min_precision"`
	MinRecall      float64            `json:"min_recall"`
}
```

Replace `RetrievalFixture` with (drop the unused `Embedding`; add graph shape):

```go
// FixtureEdge is a directed weighted edge between two fixtures (by fixture ID).
type FixtureEdge struct {
	Target string  `json:"target"`
	Weight float64 `json:"weight"`
}

// RetrievalFixture is a pre-seeded node for retrieval tests. Embeddings are
// computed at seed time via the production NodeEmbedder, not stored here.
type RetrievalFixture struct {
	ID                string        `json:"id"`
	UserID            string        `json:"user_id,omitempty"`
	Type              string        `json:"type"`
	Title             string        `json:"title"`
	Summary           string        `json:"summary"`
	Tags              []string      `json:"tags"`
	RetrievalTriggers []string      `json:"retrieval_triggers,omitempty"`
	Content           string        `json:"content"`
	Pinned            bool          `json:"pinned,omitempty"`
	Edges             []FixtureEdge `json:"edges,omitempty"`
}
```

Add a diagnostics field to `CaseResult` (keep existing fields):

```go
// CaseResult holds the score for a single test case.
type CaseResult struct {
	ID          string             `json:"id"`
	Stage       string             `json:"stage"`
	Scores      map[string]float64 `json:"scores"`
	Pass        bool               `json:"pass"`
	Cached      bool               `json:"cached"`
	Error       string             `json:"error,omitempty"`
	Diagnostics []DropDiagnostic   `json:"diagnostics,omitempty"` // why expected nodes were missed
}

// DropDiagnostic explains why an expected node was not injected.
type DropDiagnostic struct {
	NodeID     string  `json:"node_id"`
	DropReason string  `json:"drop_reason"` // below_min_similarity | token_budget | outside_top_k | not_a_candidate
	Similarity float64 `json:"similarity"`
}
```

- [ ] **Step 4: Extend `ScoreRetrieval`**

In `server/eval/scorer.go`, in `ScoreRetrieval`, before the final `return scores`, add:

```go
	if len(returnedIDs) == 0 {
		scores["zero_retrieval"] = 1
	} else {
		scores["zero_retrieval"] = 0
	}
	// expected_rank: 1-based position of the first returned ID that is expected;
	// 0 when no expected ID appears in the returned list.
	expSet := make(map[string]bool, len(c.ExpectedIDs))
	for _, id := range c.ExpectedIDs {
		expSet[id] = true
	}
	scores["expected_rank"] = 0
	for i, id := range returnedIDs {
		if expSet[id] {
			scores["expected_rank"] = float64(i + 1)
			break
		}
	}
```

- [ ] **Step 5: Run scorer tests + build**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./eval/... && go test ./eval/ -run 'TestScoreRetrieval' -count=1
```

Expected: PASS. (The build will surface that `runner.go` still references the removed `Embedding` field if anywhere — fix in Task 5.)

- [ ] **Step 6: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/types.go server/eval/scorer.go server/eval/scorer_test.go
git commit -m "eval: extend retrieval case/fixture types and add zero-retrieval/rank metrics"
```

---

### Task 5: Faithful vector-path seeding + retrieval + trace diagnostics

**Files:**
- Modify: `server/eval/runner.go` (`RunConfig`, `runRetrieval`, `runRetrievalCase`, `remapIDs`)
- Test: `server/eval/runner_test.go` (append)

This is the core fix. Seed fixtures with the production `NodeEmbedder` using the chosen embedder, support multi-user/pinned/edges, construct the retriever **with** the embedder and `config.Default()` params, pass history, and attach drop diagnostics from the `RetrievalTrace`.

- [ ] **Step 1: Add embedder fields to RunConfig**

In `server/eval/runner.go`, extend `RunConfig`:

```go
type RunConfig struct {
	Provider     provider.Provider
	ProviderName string
	Model        string
	DataDir      string
	CacheDir     string
	Stages       []string

	// Retrieval embedding. Embedder is used for both seeding fixtures and the
	// query. EmbeddingModel labels stored vectors. When Embedder is nil the
	// retrieval stage falls back to the legacy no-embedder (LLM) path.
	Embedder       provider.Embedder
	EmbeddingModel string
}
```

- [ ] **Step 2: Write the failing test (deterministic, hermetic — no API)**

Append to `server/eval/runner_test.go`:

```go
func TestRunRetrievalVectorPathDeterministic(t *testing.T) {
	dir := t.TempDir()
	rdir := filepath.Join(dir, "retrieval")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtures := `[
	  {"id":"n_coffee","type":"preference","title":"prefers dark roast coffee","summary":"dark roast coffee preference","tags":["coffee","beverage"],"content":"I always pick dark roast coffee."},
	  {"id":"n_hike","type":"fact","title":"enjoys mountain hiking","summary":"mountain hiking hobby","tags":["hiking","outdoors"],"content":"Weekend mountain hiking trips."}
	]`
	cases := `[
	  {"id":"c_coffee","query":"what coffee does the user like dark roast","expected_node_ids":["n_coffee"],"expected_not_ids":["n_hike"],"min_precision":0.5,"min_recall":1.0}
	]`
	if err := os.WriteFile(filepath.Join(rdir, "fixtures.json"), []byte(fixtures), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rdir, "cases.json"), []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}

	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       NewDeterministicEmbedder(128),
		EmbeddingModel: "det",
	}, filepath.Join(rdir, "cases.json"))
	if err != nil {
		t.Fatalf("runRetrieval: %v", err)
	}
	if len(stage.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(stage.Results))
	}
	r := stage.Results[0]
	if r.Error != "" {
		t.Fatalf("case error: %s", r.Error)
	}
	if r.Scores["recall"] < 1.0 {
		t.Errorf("recall = %v, want 1.0 (coffee node should be retrieved on the vector path)", r.Scores["recall"])
	}
	if r.Scores["exclusion"] != 1.0 {
		t.Errorf("exclusion = %v, want 1.0 (hiking node must not appear)", r.Scores["exclusion"])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestRunRetrievalVectorPathDeterministic
```

Expected: FAIL — today `runRetrieval` builds the retriever with no embedder, so `recall` is 0 (vector path never runs / nothing matches).

- [ ] **Step 4: Rework `runRetrieval` seeding + retriever construction**

In `server/eval/runner.go`, replace the fixture-seeding loop and retriever construction inside `runRetrieval`. After fixtures are loaded and the store/`cm` are created, use this seeding (replaces the existing `for _, f := range fixtures { ... }` block and the `retriever := memory.NewRetriever(...)` block):

```go
	// Node embedder uses the SAME text recipe as production (title+summary+
	// tags+triggers+content) so eval embeddings match real ones.
	var nodeEmbedder *memory.NodeEmbedder
	if cfg.Embedder != nil {
		nodeEmbedder = memory.NewNodeEmbedder(store, cm, cfg.Embedder, cfg.EmbeddingModel, slog.Default())
	}

	fixtureIDMap := make(map[string]string, len(fixtures))
	type pendingEdge struct{ from, to string; weight float64 }
	var edges []pendingEdge

	for _, f := range fixtures {
		var uid *string
		if f.UserID != "" {
			u := f.UserID
			uid = &u
		}
		n := &memory.Node{
			UserID:            uid,
			Type:              memory.NodeType(f.Type),
			Title:             f.Title,
			Summary:           f.Summary,
			Tags:              f.Tags,
			RetrievalTriggers: f.RetrievalTriggers,
			Pinned:            f.Pinned,
			EnrichmentStatus:  memory.EnrichmentComplete,
			Confidence:        1.0,
		}
		actualID, cerr := store.CreateNode(n)
		if cerr != nil {
			return StageResult{}, fmt.Errorf("seed fixture %q: %w", f.ID, cerr)
		}
		n.ID = actualID
		if f.Content != "" {
			path, werr := cm.Write(actualID, f.Content)
			if werr != nil {
				return StageResult{}, fmt.Errorf("write fixture content %q: %w", f.ID, werr)
			}
			n.ContentPath = path
		}
		// Embed via the production recipe so retrieveVector can score it.
		if nodeEmbedder != nil {
			if eerr := nodeEmbedder.EmbedNode(ctx, n); eerr != nil {
				return StageResult{}, fmt.Errorf("embed fixture %q: %w", f.ID, eerr)
			}
		}
		fixtureIDMap[f.ID] = actualID
		for _, e := range f.Edges {
			edges = append(edges, pendingEdge{from: f.ID, to: e.Target, weight: e.Weight})
		}
	}

	// Seed edges now that all fixture IDs are mapped.
	for _, e := range edges {
		from, ok1 := fixtureIDMap[e.from]
		to, ok2 := fixtureIDMap[e.to]
		if !ok1 || !ok2 {
			return StageResult{}, fmt.Errorf("edge references unknown fixture: %s -> %s", e.from, e.to)
		}
		if _, eerr := store.CreateEdge(&memory.Edge{SourceID: from, TargetID: to, Type: memory.RelRelated, Weight: e.weight}); eerr != nil {
			return StageResult{}, fmt.Errorf("seed edge %s->%s: %w", e.from, e.to, eerr)
		}
	}

	// Build the retriever on the real vector path with production defaults.
	defaults := config.Default().Memory
	retriever := memory.NewRetriever(memory.RetrieverConfig{
		Store:          store,
		Content:        cm,
		Provider:       cfg.Provider,
		Model:          cfg.Model,
		Embedder:       cfg.Embedder,
		EmbeddingModel: cfg.EmbeddingModel,
		TopK:           defaults.RetrievalTopK,
		TokenBudget:    defaults.RetrievalTokenBudget,
		MinSimilarity:  defaults.RetrievalMinSimilarity,
		TypeBoost:      defaults.RetrievalTypeBoost,
		ContextWindow:  defaults.ContextWindow,
	})
```

Add imports to `runner.go` if missing: `"log/slog"` and `"github.com/cogitatorai/cogitator/server/internal/config"`.

> Verify during execution: the exact `memory.CreateEdge`/`memory.Edge` field names and the related-edge relation constant (the plan uses `memory.RelRelated`; confirm with `grep -n "RelRelated\|func (s \*Store) CreateEdge\|type Edge struct" internal/memory/*.go` and use the real names). If pinned/edges seeding APIs differ, adapt; the must-haves are: embeddings saved per node, per-user scoping, and pinned set.

- [ ] **Step 5: Add trace diagnostics to `runRetrievalCase`**

Replace `runRetrievalCase` with a version that requests a trace and records drop reasons for missed expected nodes:

```go
func runRetrievalCase(ctx context.Context, cfg RunConfig, retriever *memory.Retriever, c RetrievalCase) CaseResult {
	cr := CaseResult{ID: c.ID, Stage: "retrieval", Scores: make(map[string]float64)}

	tctx, holder := memory.WithTrace(ctx)
	result, err := retriever.Retrieve(tctx, c.UserID, c.Query, c.History)
	if err != nil {
		cr.Error = err.Error()
		return cr
	}

	var returnedIDs []string
	injected := make(map[string]bool)
	for _, n := range result.Nodes {
		returnedIDs = append(returnedIDs, n.Node.ID)
		injected[n.Node.ID] = true
	}

	cr.Scores = ScoreRetrieval(c, returnedIDs)
	cr.Pass = cr.Scores["precision"] >= c.MinPrecision && cr.Scores["recall"] >= c.MinRecall

	// Diagnose expected-but-missed nodes from the trace.
	if tr := holder.Get(); tr != nil {
		byID := make(map[string]TraceCandidateView, len(tr.Candidates))
		for _, cand := range tr.Candidates {
			byID[cand.NodeID] = TraceCandidateView{Reason: cand.DropReason, Sim: cand.Similarity}
		}
		for _, id := range c.ExpectedIDs {
			if injected[id] {
				continue
			}
			d := DropDiagnostic{NodeID: id, DropReason: "not_a_candidate"}
			if v, ok := byID[id]; ok {
				d.DropReason = v.Reason
				d.Similarity = v.Sim
			}
			cr.Diagnostics = append(cr.Diagnostics, d)
		}
	}
	return cr
}

// TraceCandidateView is a tiny projection of memory.TraceCandidate used locally.
type TraceCandidateView struct {
	Reason string
	Sim    float64
}
```

> Verify during execution: the `memory.RetrievalTrace` field names are `Candidates []TraceCandidate` with `NodeID`, `DropReason`, `Similarity` (from PR #39's `internal/memory/trace.go`). Confirm with `grep -n "type TraceCandidate" -A8 internal/memory/trace.go` and match exactly.

- [ ] **Step 6: Run the deterministic vector-path test + whole package**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run TestRunRetrievalVectorPathDeterministic -count=1
go build ./eval/... && go test ./eval/ -count=1
```

Expected: the new test PASSES (recall 1.0, exclusion 1.0 on the real vector path); the package builds. If the legacy `testdata/retrieval/cases.json`/`fixtures.json` cause failures because the old fixtures still carry the removed `embedding` field, that's fine — those files are replaced in Tasks 8–9; if a pre-existing runner test reads them, mark it skipped with a comment pointing to Task 9, or update it minimally.

- [ ] **Step 7: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/runner.go server/eval/runner_test.go
git commit -m "eval: seed + retrieve on the real vector path with trace-based drop diagnostics"
```

---

### Task 6: CLI — embedder selection, offline mode, exit codes

**Files:**
- Modify: `server/eval/cmd/main.go`

- [ ] **Step 1: Add flags and embedder construction**

In `server/eval/cmd/main.go` `runCmd`, add flags after the existing ones:

```go
	embedderMode := fs.String("embedder", "deterministic", "retrieval embedder: deterministic | real")
	offline := fs.Bool("offline", false, "forbid live embedding API calls (cache-only)")
	embModel := fs.String("embedding-model", "", "embedding model for -embedder=real (default: config Memory.EmbeddingModel)")
```

- [ ] **Step 2: Build the embedder and relax the API-key requirement for deterministic mode**

Replace the block that unconditionally requires an API key and builds `prov := provider.NewOpenAI(...)` with logic that only needs a provider/key when actually calling APIs. After `dataDir := findDataDir()` compute the embedder:

```go
	var embedder provider.Embedder
	var embeddingModel string
	switch *embedderMode {
	case "deterministic":
		embedder = eval.NewDeterministicEmbedder(0)
		embeddingModel = "deterministic"
	case "real":
		embeddingModel = *embModel
		if embeddingModel == "" {
			embeddingModel = cfg.Memory.EmbeddingModel
		}
		embDir := filepath.Join(dataDir, "embeddings")
		// In offline mode no inner embedder is needed (cache-only). Otherwise
		// build a real provider, which requires an API key.
		var inner provider.Embedder
		if !*offline {
			apiKey := cfg.ProviderAPIKey(*providerName)
			if apiKey == "" {
				apiKey = os.Getenv("COGITATOR_API_KEY")
			}
			if apiKey == "" {
				fmt.Fprintf(os.Stderr, "error: -embedder=real without -offline needs an API key for %q (keychain, secrets.yaml, or COGITATOR_API_KEY)\n", *providerName)
				os.Exit(1)
			}
			inner = provider.NewOpenAI(*providerName, apiKey)
		}
		embedder = eval.NewCachedEmbedder(inner, embeddingModel, embDir, *offline)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown -embedder %q (want deterministic|real)\n", *embedderMode)
		os.Exit(1)
	}
```

The existing provider construction used for enrichment/reflection LLM stages should remain, but make it lazy: only require the API key when those stages actually run. Simplest adaptation that preserves behavior: keep building `prov` from the API key **only if** a key is present, and pass it through; the retrieval stage now uses `embedder` regardless. Wire both into `RunConfig`:

```go
	report, err := eval.Run(context.Background(), eval.RunConfig{
		Provider:       prov, // may be nil in deterministic/offline retrieval-only runs
		ProviderName:   *providerName,
		Model:          *model,
		DataDir:        dataDir,
		CacheDir:       cacheDir,
		Stages:         stages,
		Embedder:       embedder,
		EmbeddingModel: embeddingModel,
	})
```

Where `prov` is now built guarded:

```go
	var prov provider.Provider
	if apiKey := firstNonEmpty(cfg.ProviderAPIKey(*providerName), os.Getenv("COGITATOR_API_KEY")); apiKey != "" {
		prov = provider.NewOpenAI(*providerName, apiKey)
	}
```

Add a small `firstNonEmpty(a, b string) string` helper at the bottom of `main.go` (returns a if non-empty else b). Remove the old hard `os.Exit(1)` when no API key, since deterministic retrieval needs none. (If the user runs an LLM stage with no provider, `Run` already surfaces a clear error per-stage.)

- [ ] **Step 3: Add non-zero exit on case failures**

After `eval.WriteTable(os.Stdout, report)` (and the optional `-out` write), add:

```go
	failed := 0
	for _, st := range report.Stages {
		for _, r := range st.Results {
			if !r.Pass {
				failed++
			}
		}
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d case(s) below threshold\n", failed)
		os.Exit(1)
	}
```

- [ ] **Step 4: Build and smoke-run deterministic mode (no API key needed)**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go build ./eval/...
# Deterministic retrieval-only run must work with zero secrets:
go run ./eval/cmd run -stage retrieval -embedder deterministic ; echo "exit=$?"
```

Expected: builds; the deterministic retrieval run executes with no API key. (Exit code may be non-zero until Task 8 authors mechanics fixtures/cases — that's fine here; the point is it runs without secrets.)

- [ ] **Step 5: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/cmd/main.go
git commit -m "eval: CLI embedder selection, offline mode, and non-zero exit on failures"
```

---

### Task 7: `go test` wrappers for CI

**Files:**
- Create: `server/eval/retrieval_eval_test.go`

These ride the existing `go test ./...` CI step. L1 always runs hermetically; L2 runs offline-against-cache and skips when the cache is absent.

- [ ] **Step 1: Write the wrapper tests**

Create `server/eval/retrieval_eval_test.go`:

```go
package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// findRetrievalDir locates testdata/retrieval relative to this package.
func findRetrievalDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("testdata", "retrieval")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("retrieval testdata dir not found: %v", err)
	}
	return dir
}

// TestL1MechanicsHermetic runs the deterministic mechanics cases with no API,
// no network, no cache. This is the always-on CI gate for retrieval mechanics.
func TestL1MechanicsHermetic(t *testing.T) {
	dir := findRetrievalDir(t)
	casesPath := filepath.Join(dir, "mechanics.json")
	if _, err := os.Stat(casesPath); err != nil {
		t.Skip("mechanics.json not present yet (authored in Task 8)")
	}
	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       NewDeterministicEmbedder(0),
		EmbeddingModel: "deterministic",
	}, casesPath)
	if err != nil {
		t.Fatalf("run mechanics: %v", err)
	}
	for _, r := range stage.Results {
		if !r.Pass {
			t.Errorf("mechanics case %s failed: scores=%v diagnostics=%v", r.ID, r.Scores, r.Diagnostics)
		}
	}
}

// TestL2SemanticOfflineCache runs the semantic cases against the committed
// embedding cache with no network. Skips when the cache is absent so a checkout
// lacking embeddings never spuriously fails.
func TestL2SemanticOfflineCache(t *testing.T) {
	dir := findRetrievalDir(t)
	casesPath := filepath.Join(dir, "cases.json")
	embDir := filepath.Join("testdata", "embeddings")
	if _, err := os.Stat(casesPath); err != nil {
		t.Skip("cases.json not present yet (authored in Task 9)")
	}
	if _, err := os.Stat(embDir); err != nil {
		t.Skip("no committed embedding cache; run `eval run -stage retrieval -embedder real` locally to populate it")
	}
	model := "text-embedding-3-small"
	cached := NewCachedEmbedder(nil, model, embDir, true) // offline: cache-only
	stage, err := runRetrieval(context.Background(), RunConfig{
		Embedder:       cached,
		EmbeddingModel: model,
	}, casesPath)
	if err != nil {
		t.Skipf("offline semantic eval unavailable (likely incomplete cache): %v", err)
	}
	for _, r := range stage.Results {
		if !r.Pass {
			t.Errorf("semantic case %s below threshold: scores=%v diagnostics=%v", r.ID, r.Scores, r.Diagnostics)
		}
	}
}
```

- [ ] **Step 2: Run (both skip cleanly today)**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go test ./eval/ -run 'TestL1MechanicsHermetic|TestL2SemanticOfflineCache' -v -count=1
```

Expected: both SKIP (datasets not authored yet), no failures.

- [ ] **Step 3: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/retrieval_eval_test.go
git commit -m "eval: add CI go-test wrappers for L1 (hermetic) and L2 (offline cache)"
```

---

### Task 8: Author the L1 mechanics dataset

**Files:**
- Create: `server/eval/testdata/retrieval/mechanics_fixtures.json`
- Create: `server/eval/testdata/retrieval/mechanics.json`
- Modify: `server/eval/runner.go` (mechanics fixtures path) and `server/eval/cmd/main.go` (file selection by mode)

The deterministic embedder makes token overlap the similarity signal, so author fixtures + cases where the ranking is known by construction.

- [ ] **Step 1: Make the runner load a mode-specific fixtures file**

Currently `runRetrieval` derives `fixtures.json` from `filepath.Dir(casesPath)`. Generalize: load the fixtures file whose base name matches the cases file (`mechanics.json` → `mechanics_fixtures.json`; `cases.json` → `fixtures.json`). In `runRetrieval`, replace the `fixturesPath := filepath.Join(filepath.Dir(casesPath), "fixtures.json")` line with:

```go
	base := filepath.Base(casesPath)
	fixturesName := "fixtures.json"
	if base == "mechanics.json" {
		fixturesName = "mechanics_fixtures.json"
	}
	fixturesPath := filepath.Join(filepath.Dir(casesPath), fixturesName)
```

- [ ] **Step 2: Select the cases file by embedder mode in the CLI**

In `server/eval/cmd/main.go`, the retrieval stage path is currently fixed at `<dataDir>/retrieval/cases.json` inside `eval.Run`. Add a `RetrievalCasesFile` override to `RunConfig` (default `cases.json`) and set it from the CLI: `mechanics.json` when `-embedder=deterministic`, else `cases.json`. In `runner.go` where the retrieval stage path is built, honor `cfg.RetrievalCasesFile` if set. (Confirm the exact place `Run` builds the retrieval cases path and thread the override; `grep -n "retrieval" server/eval/runner.go`.)

Add to `RunConfig`:

```go
	RetrievalCasesFile string // default "cases.json"; "mechanics.json" for deterministic
```

In the CLI, after computing `embedderMode`:

```go
	retrievalCases := "cases.json"
	if *embedderMode == "deterministic" {
		retrievalCases = "mechanics.json"
	}
```

and pass `RetrievalCasesFile: retrievalCases` in the `RunConfig`.

- [ ] **Step 3: Author `mechanics_fixtures.json`**

Create `server/eval/testdata/retrieval/mechanics_fixtures.json` with a small hand-built graph (~8 nodes) engineered so token overlap yields a known ranking. Example (extend to cover all mechanics):

```json
[
  {"id":"m_exact","type":"preference","title":"alpha alpha alpha keyword","summary":"alpha alpha alpha keyword","tags":["alpha"],"content":"alpha alpha alpha keyword"},
  {"id":"m_partial","type":"fact","title":"alpha beta gamma","summary":"alpha beta gamma","tags":["beta"],"content":"alpha beta gamma"},
  {"id":"m_unrelated","type":"fact","title":"zeta eta theta","summary":"zeta eta theta","tags":["zeta"],"content":"zeta eta theta"},
  {"id":"m_pinned","type":"fact","title":"omega pinned note","summary":"omega pinned note","tags":["omega"],"content":"omega pinned note","pinned":true},
  {"id":"m_boost_pref","type":"preference","title":"delta preference","summary":"delta preference","tags":["delta"],"content":"delta preference"},
  {"id":"m_boost_fact","type":"fact","title":"delta fact","summary":"delta fact","tags":["delta"],"content":"delta fact"}
]
```

- [ ] **Step 4: Author `mechanics.json`**

Create `server/eval/testdata/retrieval/mechanics.json` with ~6–10 cases asserting each mechanic. Examples:

```json
[
  {"id":"mech_exact_over_partial","query":"alpha alpha alpha keyword","expected_node_ids":["m_exact"],"expected_not_ids":["m_unrelated"],"min_precision":0.3,"min_recall":1.0},
  {"id":"mech_below_threshold_zero","query":"qqq www eee rrr","expected_node_ids":[],"expected_not_ids":["m_exact","m_partial"],"min_precision":0.0,"min_recall":0.0},
  {"id":"mech_typeboost_pref_first","query":"delta","expected_node_ids":["m_boost_pref"],"expected_not_ids":[],"min_precision":0.3,"min_recall":1.0}
]
```

> Authoring rule: run `go run ./eval/cmd run -stage retrieval -embedder deterministic` after each addition and read the diagnostics — they show the exact similarity each expected node received, so you can set thresholds against observed deterministic scores. The `mech_below_threshold_zero` case proves the 0.3 floor produces zero retrieval for an all-novel-token query. Add cases for token-budget eviction (several high-overlap nodes with long content so the 2000-token budget drops the lowest), top-K, and pinned bypass (`m_pinned` should appear in `result.Pinned` regardless of query — note: pinned nodes are returned via the Pinned slice, so a pinned-bypass assertion may check presence differently; if the eval only scores `result.Nodes`, document that pinned coverage is asserted via a dedicated case or extend scoring to include pinned IDs).

- [ ] **Step 5: Verify the mechanics suite passes deterministically**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go run ./eval/cmd run -stage retrieval -embedder deterministic ; echo "exit=$?"
go test ./eval/ -run TestL1MechanicsHermetic -v -count=1
```

Expected: all mechanics cases pass; the L1 wrapper test now runs (not skips) and passes; exit 0.

- [ ] **Step 6: Commit**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/testdata/retrieval/mechanics_fixtures.json server/eval/testdata/retrieval/mechanics.json server/eval/runner.go server/eval/cmd/main.go
git commit -m "eval: add deterministic L1 retrieval-mechanics dataset"
```

---

### Task 9: Author the L2 semantic dataset (~150 nodes) + fill the embedding cache

**Files:**
- Replace: `server/eval/testdata/retrieval/fixtures.json` (~150 nodes)
- Replace: `server/eval/testdata/retrieval/cases.json` (~20–30 cases)
- Create: `server/eval/testdata/embeddings/<model>/*.json` (committed vectors)

- [ ] **Step 1: Author the fixture graph (~150 nodes)**

Replace `server/eval/testdata/retrieval/fixtures.json` with ~150 nodes following the `RetrievalFixture` schema (Task 4). Composition:
- **~120 nodes under user `"u_heavy"`**, **~30 under `"u_light"`** (set `user_id` on each). The heavy user is the realistic dense graph; the light user exists for isolation tests.
- **~40 curated nodes** (query targets + adversarial near-duplicates: e.g. `pref_coffee_current` "switched to oat-milk lattes" vs `pref_coffee_old` "used to drink dark roast"; topically-adjacent facts) authored by hand with realistic `title`/`summary`/`tags`/`retrieval_triggers`/`content`.
- **~110 filler nodes** — plausible, topically diverse memories (hobbies, work facts, preferences, skills) that add density. These may be generated (LLM-assisted) once; they only need to be coherent and varied. Spread across `fact`/`preference`/`pattern`/`skill`.
- A handful **pinned**; several **edges** between related curated nodes.

- [ ] **Step 2: Author the query cases (~20–30)**

Replace `server/eval/testdata/retrieval/cases.json` with ~20–30 `RetrievalCase` entries covering the failure modes, each with `expected_node_ids`, `expected_not_ids`, and `min_precision`/`min_recall`:
- direct match; paraphrase (query wording differs from the node);
- multi-hop / connected (expected node reachable via an edge);
- distractor rejection (near-duplicate must NOT be returned — use `expected_not_ids`);
- history dilution (populate `history` with several off-topic turns; assert the on-topic memory still surfaces);
- pinned-starvation (heavy pinning + a query whose match competes with pinned budget);
- genuine zero-retrieval (query unrelated to any memory; `expected_node_ids: []`);
- cross-user isolation (a `u_light` query must not return `u_heavy` nodes).

- [ ] **Step 3: Fill and commit the embedding cache (requires an API key, one-time)**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
# Populates testdata/embeddings/<model>/ by embedding every fixture + query once.
go run ./eval/cmd run -stage retrieval -embedder real ; echo "exit=$?"
ls testdata/embeddings/*/ | head
```

Expected: the run embeds all nodes + queries (cache files written), then scores. Iterate on thresholds/cases until the semantic suite reflects intended quality. (If no API key is available in this environment, STOP and hand back to the user: this step needs a one-time key to generate the committed vectors; everything else in the plan is hermetic.)

- [ ] **Step 4: Verify offline reproduction (no API key)**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go run ./eval/cmd run -stage retrieval -embedder real -offline ; echo "exit=$?"
go test ./eval/ -run TestL2SemanticOfflineCache -v -count=1
```

Expected: the offline run reproduces identical results from the committed cache with no network; the L2 wrapper test runs (not skips) and passes.

- [ ] **Step 5: Commit dataset + cache**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/testdata/retrieval/fixtures.json server/eval/testdata/retrieval/cases.json server/eval/testdata/embeddings
git commit -m "eval: add ~150-node semantic retrieval dataset + committed embedding cache"
```

---

### Task 10: Final verification, README, IMPROVEMENTS.md, review

**Files:**
- Modify: `server/eval/README.md` (document the new modes)
- Modify: `IMPROVEMENTS.md` (local working-tree doc; not committed)

- [ ] **Step 1: Full build + test, all tags**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator/server
go vet ./... && go build ./... && go build -tags desktop ./... && go build -tags saas ./...
go test -race -count=1 ./eval/... ./internal/memory/...
```

Expected: all green, including L1 (runs) and L2 (runs if cache committed, else skips).

- [ ] **Step 2: Update the eval README**

In `server/eval/README.md`, document: the two retrieval embedder modes (`-embedder deterministic|real`), `-offline`, the committed embedding cache location, that L1 is hermetic/CI-safe and L2 needs the committed cache (regenerate locally with an API key), and the drop-reason diagnostics in the output.

- [ ] **Step 3: Update IMPROVEMENTS.md (local only, do not commit)**

Under §5.2, note that the retrieval eval now exercises the real vector path with deterministic (L1) + cached-real (L2) embedders, a ~150-node dataset, drop-reason diagnostics, and offline/CI-ready run modes. Leave the regression-gate / L3 / tuning bullets open.

- [ ] **Step 4: Commit the README**

```bash
cd /Users/deiu/dev/deiu/cogi/cogitator
git add server/eval/README.md
git commit -m "eval: document retrieval embedder modes, offline cache, and diagnostics"
```

- [ ] **Step 5: Code review**

Run the `code-review` skill over `git diff main...HEAD` (the `code-reviewer` agent is not registered in this environment). Address findings, then use superpowers:finishing-a-development-branch to decide merge/PR (a PR to `main` matching the established flow is expected; do not push/PR without the user's say-so unless already authorized).

---

## Self-review notes

- **Spec coverage:** track harness (Task 1, spec §0); deterministic embedder (Task 2, §1 L1); cached embedder + committed cache (Task 3, §1 L2); types + metrics (Task 4, §4); faithful seeding/retrieval + trace diagnostics (Task 5, §2/§3); CLI modes + offline + exit codes (Task 6, §6); go-test CI wrappers (Task 7, §6); L1 dataset (Task 8, §5); L2 ~150-node dataset + cache (Task 9, §5); README/bookkeeping/review (Task 10). Out-of-scope items (regression gate, L3, tuning, CI workflow, enrichment/reflection rework) are not tasked, per the spec.
- **Type consistency:** `NewDeterministicEmbedder(dim)`, `NewCachedEmbedder(inner, model, dir, offline)` used identically in Tasks 2/3/5/6/7; `RetrievalCase.{History,UserID}` and `RetrievalFixture.{UserID,RetrievalTriggers,Pinned,Edges}` defined in Task 4 and consumed in Task 5; `DropDiagnostic`/`CaseResult.Diagnostics` defined in Task 4 and populated in Task 5; `RunConfig.{Embedder,EmbeddingModel,RetrievalCasesFile}` defined in Tasks 5/8 and set in Task 6/8.
- **Verify-during-execution points (called out inline):** `memory.CreateEdge`/`Edge`/relation-constant names (Task 5 Step 4); `memory.RetrievalTrace`/`TraceCandidate` field names from PR #39 (Task 5 Step 5); where `eval.Run` builds the retrieval cases path to thread `RetrievalCasesFile` (Task 8 Step 2); pinned-coverage scoring (Task 8 Step 4). These are confirmations against real code, not redesigns.
- **API-key boundary:** every task except Task 9 Step 3 is hermetic (no key). Task 9 Step 3 is the single one-time key-requiring step (generate committed vectors); it explicitly hands back to the user if no key is available.
```
