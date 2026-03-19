package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
)

// signalsJSON builds a JSON string matching the reflectionResponse shape.
func signalsJSON(t *testing.T, signals []map[string]any) string {
	t.Helper()
	payload := map[string]any{"signals": signals}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal signalsJSON: %v", err)
	}
	return string(b)
}

// TestReflectorExtractsSignals verifies that pattern-based detection fires for
// English messages and that an episode node is created without any LLM call.
func TestReflectorExtractsSignals(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	const sessionKey = "test-session-1"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat1", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// "perfect" triggers an acknowledgment pattern; "that's wrong" triggers a
	// correction pattern. Both follow assistant turns, so two signals are expected.
	msgs := []session.Message{
		{Role: "user", Content: "Explain how goroutines work."},
		{Role: "assistant", Content: "Goroutines are lightweight threads managed by the Go runtime."},
		{Role: "user", Content: "That's wrong, goroutines are not OS threads."},
		{Role: "assistant", Content: "You are right. Goroutines are multiplexed onto OS threads by the scheduler."},
		{Role: "user", Content: "Perfect, that is exactly what I needed."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	// The mock should never be called for English messages; configure it to
	// return something valid just in case, to avoid a panic if the test fails.
	mock := provider.NewMock(provider.Response{Content: signalsJSON(t, []map[string]any{})})
	contentDir := filepath.Join(t.TempDir(), "content")
	cm := memory.NewContentManager(contentDir)

	reflector := NewReflector(sessStore, memStore, cm, mock, eventBus, "test-model", 20, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reflector.Start(ctx)
	defer reflector.Stop()

	eventBus.Publish(bus.Event{
		Type:    bus.ReflectionTriggered,
		Payload: map[string]any{"session_key": sessionKey},
	})

	// Wait until two episode nodes appear in the store.
	waitFor(t, 3*time.Second, func() bool {
		nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
		return err == nil && len(nodes) >= 2
	})

	nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
	if err != nil {
		t.Fatalf("list episode nodes: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected at least 2 episode nodes, got %d", len(nodes))
	}

	// Pattern-based detection must not have touched the LLM.
	if n := mock.CallCount(); n != 0 {
		t.Errorf("expected 0 LLM calls for English messages, got %d", n)
	}
}

// TestReflectorNoSignals verifies that conversations with no behavioral signal
// patterns produce no episode nodes and no LLM call.
func TestReflectorNoSignals(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	const sessionKey = "test-session-2"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat2", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	msgs := []session.Message{
		{Role: "user", Content: "What is 2 + 2?"},
		{Role: "assistant", Content: "4."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	mock := provider.NewMock(provider.Response{Content: signalsJSON(t, []map[string]any{})})

	reflector := NewReflector(sessStore, memStore, nil, mock, eventBus, "test-model", 20, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reflector.Start(ctx)
	defer reflector.Stop()

	eventBus.Publish(bus.Event{
		Type:    bus.ReflectionTriggered,
		Payload: map[string]any{"session_key": sessionKey},
	})

	// Give the goroutine time to process the event.
	time.Sleep(200 * time.Millisecond)

	nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
	if err != nil {
		t.Fatalf("list episode nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 episode nodes for empty signals, got %d", len(nodes))
	}

	// English path must not invoke the LLM.
	if n := mock.CallCount(); n != 0 {
		t.Errorf("expected 0 LLM calls for English messages, got %d", n)
	}
}

// TestReflectorQueuesEnrichment verifies that a detected refinement signal
// triggers an enrichment.queued event with a valid node_id.
func TestReflectorQueuesEnrichment(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	enrichCh := eventBus.Subscribe(bus.EnrichmentQueued)

	const sessionKey = "test-session-3"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat3", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// "instead of" is a refinement pattern that will be caught without LLM.
	msgs := []session.Message{
		{Role: "user", Content: "Show me an example."},
		{Role: "assistant", Content: "Here is a long example with lots of boilerplate code..."},
		{Role: "user", Content: "Instead of the full example, show only the relevant part."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	mock := provider.NewMock(provider.Response{Content: signalsJSON(t, []map[string]any{})})

	reflector := NewReflector(sessStore, memStore, nil, mock, eventBus, "test-model", 20, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reflector.Start(ctx)
	defer reflector.Stop()

	eventBus.Publish(bus.Event{
		Type:    bus.ReflectionTriggered,
		Payload: map[string]any{"session_key": sessionKey},
	})

	var enrichEvents []bus.Event
	deadline := time.After(3 * time.Second)
collecting:
	for {
		select {
		case evt, ok := <-enrichCh:
			if !ok {
				break collecting
			}
			enrichEvents = append(enrichEvents, evt)
			if len(enrichEvents) >= 1 {
				break collecting
			}
		case <-deadline:
			break collecting
		}
	}

	if len(enrichEvents) == 0 {
		t.Fatal("expected at least one enrichment.queued event, got none")
	}

	nodeID, ok := enrichEvents[0].Payload["node_id"].(string)
	if !ok || nodeID == "" {
		t.Errorf("enrichment.queued event missing node_id, payload: %v", enrichEvents[0].Payload)
	}

	if nodeID != "" {
		n, err := memStore.GetNode(nodeID)
		if err != nil {
			t.Fatalf("get node %q: %v", nodeID, err)
		}
		if n.Type != memory.NodeEpisode {
			t.Errorf("node type = %q, want %q", n.Type, memory.NodeEpisode)
		}
	}
}

// TestReflectorNonEnglishFallsBackToLLM verifies that non-English messages
// bypass pattern detection and use the LLM classifier instead.
func TestReflectorNonEnglishFallsBackToLLM(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	const sessionKey = "test-session-4"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat4", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Messages with a high non-ASCII ratio (>30%) force the LLM path.
	msgs := []session.Message{
		{Role: "user", Content: "Объясни как работают горутины."},
		{Role: "assistant", Content: "Горутины — это лёгкие потоки, управляемые рантаймом Go."},
		{Role: "user", Content: "Неверно, объясни подробнее."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	mockResp := signalsJSON(t, []map[string]any{
		{
			"type":           "correction",
			"summary":        "User asked for a more detailed explanation",
			"suggested_rule": "Provide detailed explanations by default",
			"confidence":     0.88,
			"message_index":  2,
		},
	})
	mock := provider.NewMock(provider.Response{Content: mockResp})

	reflector := NewReflector(sessStore, memStore, nil, mock, eventBus, "test-model", 20, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reflector.Start(ctx)
	defer reflector.Stop()

	eventBus.Publish(bus.Event{
		Type:    bus.ReflectionTriggered,
		Payload: map[string]any{"session_key": sessionKey},
	})

	// Wait for the LLM call and node creation.
	waitFor(t, 3*time.Second, func() bool {
		return mock.CallCount() > 0
	})

	if n := mock.CallCount(); n != 1 {
		t.Errorf("expected 1 LLM call for non-English messages, got %d", n)
	}

	waitFor(t, 2*time.Second, func() bool {
		nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
		return err == nil && len(nodes) >= 1
	})

	nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
	if err != nil {
		t.Fatalf("list episode nodes: %v", err)
	}
	if len(nodes) < 1 {
		t.Fatalf("expected at least 1 episode node from LLM path, got %d", len(nodes))
	}
}
