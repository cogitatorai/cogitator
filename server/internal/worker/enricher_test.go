package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// enrichJSON is a helper that serialises an enrichResult-shaped struct into a
// JSON string for use as a mock provider response.
func enrichJSON(t *testing.T, summary string, tags, triggers []string, related []map[string]any, contradictions []string) string {
	t.Helper()
	payload := map[string]any{
		"summary":            summary,
		"tags":               tags,
		"retrieval_triggers": triggers,
		"related_nodes":      related,
		"contradictions":     contradictions,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal enrichJSON: %v", err)
	}
	return string(b)
}

// waitFor polls fn until it returns true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestEnricherProcessesPendingNodes(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	// Create a pending node.
	nodeID, err := store.CreateNode(&memory.Node{
		Type:  memory.NodeFact,
		Title: "Go uses static typing",
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	responseJSON := enrichJSON(t,
		"Go is a statically typed compiled language.",
		[]string{"go", "typing", "language"},
		[]string{"what type system does Go use", "is Go statically typed"},
		nil,
		nil,
	)

	mock := provider.NewMock(provider.Response{Content: responseJSON})
	enricher := NewEnricher(store, nil, mock, eventBus, "test-model", nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	enricher.Start(ctx)
	defer enricher.Stop()

	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})

	waitFor(t, 3*time.Second, func() bool {
		n, err := store.GetNode(nodeID)
		return err == nil && n.EnrichmentStatus == memory.EnrichmentComplete
	})

	n, err := store.GetNode(nodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if n.EnrichmentStatus != memory.EnrichmentComplete {
		t.Errorf("enrichment_status = %q, want %q", n.EnrichmentStatus, memory.EnrichmentComplete)
	}
	if n.Summary == "" {
		t.Error("expected non-empty summary after enrichment")
	}
	if len(n.Tags) == 0 {
		t.Error("expected tags after enrichment")
	}
	if len(n.RetrievalTriggers) == 0 {
		t.Error("expected retrieval_triggers after enrichment")
	}
}

func TestEnricherCreatesEdges(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	// Create the target node that will be referenced as a related node.
	targetID, err := store.CreateNode(&memory.Node{
		Type:             memory.NodeFact,
		Title:            "Go has garbage collection",
		EnrichmentStatus: memory.EnrichmentComplete,
	})
	if err != nil {
		t.Fatalf("create target node: %v", err)
	}

	// Create the node to be enriched.
	sourceID, err := store.CreateNode(&memory.Node{
		Type:  memory.NodeFact,
		Title: "Go manages memory automatically",
	})
	if err != nil {
		t.Fatalf("create source node: %v", err)
	}

	related := []map[string]any{
		{"id": targetID, "relation": "supports", "weight": 0.9},
	}
	responseJSON := enrichJSON(t,
		"Go automatically manages memory via a garbage collector.",
		[]string{"go", "memory", "gc"},
		[]string{"how does Go manage memory"},
		related,
		nil,
	)

	mock := provider.NewMock(provider.Response{Content: responseJSON})
	enricher := NewEnricher(store, nil, mock, eventBus, "test-model", nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	enricher.Start(ctx)
	defer enricher.Stop()

	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})

	// Wait until both nodes in the graph have expected enrichment state.
	waitFor(t, 3*time.Second, func() bool {
		n, err := store.GetNode(sourceID)
		return err == nil && n.EnrichmentStatus == memory.EnrichmentComplete
	})

	edges, err := store.GetEdgesFrom(sourceID, "")
	if err != nil {
		t.Fatalf("get edges: %v", err)
	}
	if len(edges) == 0 {
		t.Fatal("expected at least one edge to be created")
	}

	found := false
	for _, e := range edges {
		if e.TargetID == targetID && e.Relation == memory.RelSupports {
			found = true
			if e.Weight != 0.9 {
				t.Errorf("edge weight = %v, want 0.9", e.Weight)
			}
		}
	}
	if !found {
		t.Errorf("expected edge %s -> %s (supports), not found in %v", sourceID, targetID, edges)
	}
}

func TestEnricherHandlesLLMError(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	nodeID, err := store.CreateNode(&memory.Node{
		Type:  memory.NodeFact,
		Title: "Some fact",
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	// Mock provider that returns invalid JSON so unmarshal fails, simulating a
	// broken LLM response.  The enricher should log the error and leave the
	// node's enrichment_status as "pending".
	mock := provider.NewMock(provider.Response{Content: "not valid json {"})

	enricher := NewEnricher(store, nil, mock, eventBus, "test-model", nil, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	enricher.Start(ctx)
	defer enricher.Stop()

	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})

	// Give the enricher time to process and confirm it did NOT update the node.
	// We wait long enough to be confident the goroutine ran.
	time.Sleep(200 * time.Millisecond)

	// Also wait for the mock to have been called (confirms the goroutine ran).
	waitFor(t, 2*time.Second, func() bool {
		return mock.CallCount() > 0
	})

	n, err := store.GetNode(nodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if n.EnrichmentStatus != memory.EnrichmentPending {
		t.Errorf("enrichment_status = %q after LLM error, want %q",
			n.EnrichmentStatus, memory.EnrichmentPending)
	}
}

func TestBuildEnrichmentPromptBroaderTriggers(t *testing.T) {
	node := memory.Node{
		ID:    "test-id",
		Type:  memory.NodePreference,
		Title: "Enjoys Tolkien books",
	}
	prompt := buildEnrichmentPrompt(node, "The user loves reading Tolkien novels.", "")

	if !strings.Contains(prompt, "100") {
		t.Error("prompt should mention up to 100 triggers")
	}
	if !strings.Contains(prompt, "Direct") || !strings.Contains(prompt, "Contextual") || !strings.Contains(prompt, "Lateral") {
		t.Error("prompt should mention Direct, Contextual, and Lateral trigger categories")
	}
	if !strings.Contains(prompt, "Do NOT include person names") {
		t.Error("prompt should instruct LLM to not include person names")
	}
}
