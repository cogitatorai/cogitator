package worker

import (
	"context"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

func publishMessageReceived(eb *bus.Bus, sessionKey string) {
	eb.Publish(bus.Event{
		Type: bus.MessageReceived,
		Payload: map[string]any{
			"session_key": sessionKey,
		},
	})
}

func TestReflectionTriggerMessageThreshold(t *testing.T) {
	eb := bus.New()
	defer eb.Close()

	reflectionCh := eb.Subscribe(bus.ReflectionTriggered)

	rt := NewReflectionTrigger(TriggerConfig{
		EventBus:      eb,
		MsgThreshold:  3,
		IdleTimeout:   time.Hour,
		CheckInterval: time.Hour, // disable idle checker
	})
	rt.Start(context.Background())
	defer rt.Stop()

	// Allow goroutine to start
	time.Sleep(10 * time.Millisecond)

	// Publish exactly 3 messages (threshold)
	for i := 0; i < 3; i++ {
		publishMessageReceived(eb, "sess-1")
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case evt := <-reflectionCh:
		if evt.Payload["session_key"] != "sess-1" {
			t.Errorf("expected session_key 'sess-1', got %v", evt.Payload["session_key"])
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for reflection event")
	}
}

func TestReflectionTriggerBelowThreshold(t *testing.T) {
	eb := bus.New()
	defer eb.Close()

	reflectionCh := eb.Subscribe(bus.ReflectionTriggered)

	rt := NewReflectionTrigger(TriggerConfig{
		EventBus:      eb,
		MsgThreshold:  3,
		IdleTimeout:   time.Hour,
		CheckInterval: time.Hour,
	})
	rt.Start(context.Background())
	defer rt.Stop()

	time.Sleep(10 * time.Millisecond)

	// Only 2 messages, below threshold of 3
	for i := 0; i < 2; i++ {
		publishMessageReceived(eb, "sess-2")
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-reflectionCh:
		t.Error("did not expect reflection event below threshold")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event
	}
}

func TestReflectionTriggerIdleTimeout(t *testing.T) {
	eb := bus.New()
	defer eb.Close()

	reflectionCh := eb.Subscribe(bus.ReflectionTriggered)

	rt := NewReflectionTrigger(TriggerConfig{
		EventBus:      eb,
		MsgThreshold:  100, // high threshold so count trigger won't fire
		IdleTimeout:   100 * time.Millisecond,
		CheckInterval: 50 * time.Millisecond,
	})
	rt.Start(context.Background())
	defer rt.Stop()

	time.Sleep(10 * time.Millisecond)

	// One message, then go idle
	publishMessageReceived(eb, "sess-idle")
	time.Sleep(5 * time.Millisecond)

	select {
	case evt := <-reflectionCh:
		if evt.Payload["session_key"] != "sess-idle" {
			t.Errorf("expected 'sess-idle', got %v", evt.Payload["session_key"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for idle reflection event")
	}
}

func TestReflectionTriggerResetsAfterReflection(t *testing.T) {
	eb := bus.New()
	defer eb.Close()

	reflectionCh := eb.Subscribe(bus.ReflectionTriggered)

	rt := NewReflectionTrigger(TriggerConfig{
		EventBus:      eb,
		MsgThreshold:  2,
		IdleTimeout:   time.Hour,
		CheckInterval: time.Hour,
	})
	rt.Start(context.Background())
	defer rt.Stop()

	time.Sleep(10 * time.Millisecond)

	// First 2 messages should trigger reflection
	publishMessageReceived(eb, "sess-reset")
	time.Sleep(5 * time.Millisecond)
	publishMessageReceived(eb, "sess-reset")

	select {
	case <-reflectionCh:
		// Expected: first trigger
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first reflection")
	}

	// One more message should NOT trigger (counter was reset)
	time.Sleep(10 * time.Millisecond)
	publishMessageReceived(eb, "sess-reset")

	select {
	case <-reflectionCh:
		t.Error("did not expect second reflection after only 1 message post-reset")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event
	}
}
