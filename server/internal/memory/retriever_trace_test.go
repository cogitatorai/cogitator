package memory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// TestVectorTraceClassifiesDrops seeds nodes whose embeddings yield a known
// similarity ordering and asserts the trace records injected vs dropped with
// the right reasons.
func TestVectorTraceClassifiesDrops(t *testing.T) {
	// Arrange: build a store + fake embedder using the real helpers.
	db := testDB(t)
	store := NewStore(db)

	// Query vector. Sim is cosine similarity vs each node's embedding.
	queryVec := []float32{1.0, 0.0, 0.0}

	// High-similarity node: parallel to query (sim = 1.0). Fits budget.
	idHigh, err := store.CreateNode(&Node{Type: NodeFact, Title: "high", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode high: %v", err)
	}
	store.SaveEmbedding(idHigh, []float32{1.0, 0.0, 0.0}, "test-model")
	store.UpdateContentLength(idHigh, 100)

	// Above-threshold node: sim ~= 0.707 (> 0.3) but evicted by the tiny budget.
	idMid, err := store.CreateNode(&Node{Type: NodeFact, Title: "mid", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode mid: %v", err)
	}
	store.SaveEmbedding(idMid, []float32{0.7, 0.7, 0.0}, "test-model")
	store.UpdateContentLength(idMid, 100)

	// Below-threshold node: orthogonal to query (sim = 0.0 < 0.3).
	idLow, err := store.CreateNode(&Node{Type: NodeFact, Title: "low", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode low: %v", err)
	}
	store.SaveEmbedding(idLow, []float32{0.0, 0.0, 1.0}, "test-model")
	store.UpdateContentLength(idLow, 100)

	emb := provider.NewMock()
	emb.EmbedResponse = [][]float32{queryVec}

	ring := NewTraceRing(8)
	r := NewRetriever(RetrieverConfig{
		Store:         store,
		Embedder:      emb,
		MinSimilarity: 0.3,
		TokenBudget:   10, // force a budget drop (each node estimates >10 tokens)
		TraceEnabled:  true,
		TraceSink:     ring,
	})

	_, err = r.Retrieve(context.Background(), "u1", "query text", nil)
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
	db := testDB(t)
	store := NewStore(db)

	id, err := store.CreateNode(&Node{Type: NodeFact, Title: "node", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	store.SaveEmbedding(id, []float32{1.0, 0.0, 0.0}, "test-model")
	store.UpdateContentLength(id, 100)

	emb := provider.NewMock()
	emb.EmbedResponse = [][]float32{{1.0, 0.0, 0.0}}

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

// TestLLMTraceClassifiesDrops seeds >topK node summaries with NO embedder so the
// retriever uses the llm-fallback path. The provider returns 2 of the 3 ids in
// priority order; TopK:1 forces the 1st returned id injected, the 2nd
// outside_top_k, and the unreturned 3rd llm_not_selected.
func TestLLMTraceClassifiesDrops(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id1, err := store.CreateNode(&Node{Type: NodeFact, Title: "first", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode first: %v", err)
	}
	id2, err := store.CreateNode(&Node{Type: NodeFact, Title: "second", Confidence: 0.9})
	if err != nil {
		t.Fatalf("CreateNode second: %v", err)
	}
	if _, err := store.CreateNode(&Node{Type: NodeFact, Title: "third", Confidence: 0.9}); err != nil {
		t.Fatalf("CreateNode third: %v", err)
	}

	// LLM returns id1 then id2 (priority order); id3 is never selected.
	chosen, err := json.Marshal([]string{id1, id2})
	if err != nil {
		t.Fatalf("marshal chosen ids: %v", err)
	}
	prov := provider.NewMock(provider.Response{Content: string(chosen)})

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
