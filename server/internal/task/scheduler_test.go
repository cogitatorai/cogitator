package task

import (
	"sync"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

func TestSchedulerStartsWithTasks(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{
		Name:     "every-second",
		Prompt:   "test",
		CronExpr: "@every 1s",
		Enabled:  true,
	})

	var mu sync.Mutex
	var triggered []string
	handler := func(task Task) {
		mu.Lock()
		triggered = append(triggered, task.Name)
		mu.Unlock()
	}

	eventBus := bus.New()
	defer eventBus.Close()

	sched := NewScheduler(store, handler, eventBus, nil)
	if err := sched.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer sched.Stop()

	if sched.ScheduledCount() != 1 {
		t.Errorf("expected 1 scheduled, got %d", sched.ScheduledCount())
	}

	// Wait for cron to fire
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	count := len(triggered)
	mu.Unlock()

	if count == 0 {
		t.Error("expected handler to be called at least once")
	}
}

func TestSchedulerReload(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{
		Name:     "original",
		Prompt:   "test",
		CronExpr: "@every 1h",
		Enabled:  true,
	})

	sched := NewScheduler(store, nil, nil, nil)
	sched.Start()
	defer sched.Stop()

	if sched.ScheduledCount() != 1 {
		t.Fatalf("expected 1, got %d", sched.ScheduledCount())
	}

	// Add another task and reload
	store.CreateTask(&Task{
		Name:     "second",
		Prompt:   "test",
		CronExpr: "@every 2h",
		Enabled:  true,
	})

	if err := sched.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	if sched.ScheduledCount() != 2 {
		t.Errorf("expected 2 after reload, got %d", sched.ScheduledCount())
	}
}

func TestSchedulerIgnoresDisabledTasks(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{
		Name:     "disabled",
		Prompt:   "test",
		CronExpr: "@every 1h",
		Enabled:  false,
	})

	sched := NewScheduler(store, nil, nil, nil)
	sched.Start()
	defer sched.Stop()

	if sched.ScheduledCount() != 0 {
		t.Errorf("expected 0 (disabled), got %d", sched.ScheduledCount())
	}
}

func TestSchedulerEmitsEvents(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{
		Name:     "event-task",
		Prompt:   "test",
		CronExpr: "@every 1s",
		Enabled:  true,
	})

	eventBus := bus.New()
	defer eventBus.Close()

	ch := eventBus.Subscribe(bus.TaskStarted)

	sched := NewScheduler(store, func(Task) {}, eventBus, nil)
	sched.Start()
	defer sched.Stop()

	select {
	case evt := <-ch:
		if evt.Payload["trigger"] != string(TriggerCron) {
			t.Errorf("expected 'cron' trigger, got %v", evt.Payload["trigger"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for TaskStarted event")
	}
}
