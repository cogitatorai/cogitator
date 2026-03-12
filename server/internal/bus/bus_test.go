package bus

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	defer b.Close()

	var received Event
	var wg sync.WaitGroup
	wg.Add(1)

	ch := b.Subscribe(TaskFailed)
	go func() {
		defer wg.Done()
		received = <-ch
	}()

	evt := Event{
		Type:    TaskFailed,
		Payload: map[string]any{"task_id": 42, "error": "connection refused"},
	}
	b.Publish(evt)

	wg.Wait()
	if received.Type != TaskFailed {
		t.Errorf("expected event type %s, got %s", TaskFailed, received.Type)
	}
	if received.Payload["task_id"] != 42 {
		t.Errorf("expected task_id 42, got %v", received.Payload["task_id"])
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	defer b.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex
	count := 0

	for i := 0; i < 3; i++ {
		ch := b.Subscribe(MessageReceived)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ch
			mu.Lock()
			count++
			mu.Unlock()
		}()
	}

	b.Publish(Event{Type: MessageReceived})
	wg.Wait()

	if count != 3 {
		t.Errorf("expected 3 subscribers notified, got %d", count)
	}
}

func TestSubscribeMultipleTypes(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe(TaskCompleted, TaskFailed)
	received := make([]EventType, 0, 2)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for i := 0; i < 2; i++ {
			evt := <-ch
			received = append(received, evt.Type)
		}
	}()

	b.Publish(Event{Type: TaskCompleted})
	b.Publish(Event{Type: TaskFailed})
	wg.Wait()

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
}

func TestNoDeliveryForUnsubscribedType(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe(TaskCompleted)
	b.Publish(Event{Type: TaskFailed})

	select {
	case <-ch:
		t.Error("should not receive unsubscribed event type")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe(MessageReceived)
	b.Unsubscribe(ch)

	b.Publish(Event{Type: MessageReceived})

	// Channel should be closed after unsubscribe
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestTimestampAutoSet(t *testing.T) {
	b := New()
	defer b.Close()

	ch := b.Subscribe(TaskCreated)
	var wg sync.WaitGroup
	wg.Add(1)

	var received Event
	go func() {
		defer wg.Done()
		received = <-ch
	}()

	before := time.Now()
	b.Publish(Event{Type: TaskCreated})
	wg.Wait()

	if received.Timestamp.Before(before) {
		t.Error("expected timestamp to be auto-set")
	}
}

func TestPublishAfterClose(t *testing.T) {
	b := New()
	b.Close()

	// Should not panic
	b.Publish(Event{Type: TaskCreated})
}
