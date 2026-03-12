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

func TestReflectorExtractsSignals(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	// Set up a session with a realistic correction scenario.
	const sessionKey = "test-session-1"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat1", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	msgs := []session.Message{
		{Role: "user", Content: "Explain how goroutines work."},
		{Role: "assistant", Content: "Goroutines are lightweight threads managed by the Go runtime. They are multiplexed onto OS threads by the scheduler. When a goroutine blocks, the scheduler moves other goroutines onto the available threads. The stack starts small (a few KB) and grows as needed. You can launch a goroutine with the go keyword followed by a function call. The runtime uses a work-stealing algorithm to distribute goroutines across multiple processors (GOMAXPROCS controls the number of OS threads used). Goroutines communicate using channels or shared memory protected by sync primitives. The select statement allows a goroutine to wait on multiple channel operations simultaneously..."},
		{Role: "user", Content: "Too verbose, just give me the answer in 2 sentences max."},
		{Role: "assistant", Content: "Goroutines are lightweight concurrent functions managed by the Go runtime, multiplexed onto OS threads. Launch one with the go keyword; they communicate via channels."},
		{Role: "user", Content: "Perfect, that is exactly what I needed."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	// Mock returns one correction signal and one acknowledgment.
	mockResp := signalsJSON(t, []map[string]any{
		{
			"type":           "correction",
			"summary":        "User prefers concise output over detailed explanations",
			"suggested_rule": "Default to 2-sentence answers unless asked for detail",
			"confidence":     0.9,
			"message_index":  2,
		},
		{
			"type":           "acknowledgment",
			"summary":        "User confirmed the concise format was exactly right",
			"suggested_rule": "Concise answers are preferred by this user",
			"confidence":     0.95,
			"message_index":  4,
		},
	})

	mock := provider.NewMock(provider.Response{Content: mockResp})
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

	// Verify the mock was called exactly once (one classification call).
	if n := mock.CallCount(); n != 1 {
		t.Errorf("expected 1 LLM call, got %d", n)
	}
}

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

	// Mock returns empty signals.
	emptyResp := signalsJSON(t, []map[string]any{})
	mock := provider.NewMock(provider.Response{Content: emptyResp})

	reflector := NewReflector(sessStore, memStore, nil, mock, eventBus, "test-model", 20, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reflector.Start(ctx)
	defer reflector.Stop()

	eventBus.Publish(bus.Event{
		Type:    bus.ReflectionTriggered,
		Payload: map[string]any{"session_key": sessionKey},
	})

	// Wait long enough for the goroutine to have run.
	waitFor(t, 2*time.Second, func() bool {
		return mock.CallCount() > 0
	})

	// Allow a brief window for any async node creation (there should be none).
	time.Sleep(50 * time.Millisecond)

	nodes, err := memStore.ListNodes("", memory.NodeEpisode, 10, 0)
	if err != nil {
		t.Fatalf("list episode nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 episode nodes for empty signals, got %d", len(nodes))
	}
}

func TestReflectorQueuesEnrichment(t *testing.T) {
	db := testDB(t)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	// Subscribe to enrichment.queued before starting the reflector.
	enrichCh := eventBus.Subscribe(bus.EnrichmentQueued)

	const sessionKey = "test-session-3"
	_, err := sessStore.GetOrCreate(sessionKey, "test", "chat3", "", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	msgs := []session.Message{
		{Role: "user", Content: "Show me an example."},
		{Role: "assistant", Content: "Here is a long example with lots of boilerplate code..."},
		{Role: "user", Content: "Skip the boilerplate, show only the relevant part."},
	}
	for _, m := range msgs {
		if _, err := sessStore.AddMessage(sessionKey, m); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	mockResp := signalsJSON(t, []map[string]any{
		{
			"type":           "refinement",
			"summary":        "User wants minimal examples without boilerplate",
			"suggested_rule": "Show only the relevant code, omit boilerplate",
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

	// Collect enrichment.queued events with a deadline.
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
			// We expect exactly 1 signal stored.
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

	// Verify the event carries a node_id.
	nodeID, ok := enrichEvents[0].Payload["node_id"].(string)
	if !ok || nodeID == "" {
		t.Errorf("enrichment.queued event missing node_id, payload: %v", enrichEvents[0].Payload)
	}

	// Verify the node actually exists in the store.
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
