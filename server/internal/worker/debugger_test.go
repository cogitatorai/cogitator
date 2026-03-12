package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDebuggerStartsAndStops(t *testing.T) {
	store := task.NewStore(testDB(t))
	eventBus := bus.New()
	defer eventBus.Close()

	executor := task.NewExecutor(store, nil, nil, eventBus, nil)
	debugger := NewDebugger(store, executor, eventBus, nil)

	ctx := context.Background()
	debugger.Start(ctx)
	debugger.Stop()
}

func TestDebuggerHandlesTransientFailure(t *testing.T) {
	db := testDB(t)
	store := task.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	// Create a task that will succeed on retry
	callCount := 0
	agentFn := func(_ context.Context, _, _, _, _ string) (string, error) {
		callCount++
		if callCount == 1 {
			return "", fmt.Errorf("%s", "connection refused")
		}
		return "success", nil
	}

	taskID, _ := store.CreateTask(&task.Task{
		Name:         "retry-task",
		Prompt:       "test",
		MaxRetries:   3,
		RetryBackoff: 1, // 1 second
	})

	executor := task.NewExecutor(store, agentFn, nil, eventBus, nil)
	debugger := NewDebugger(store, executor, eventBus, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	debugger.Start(ctx)
	defer debugger.Stop()

	// Execute the task (it will fail and emit TaskFailed)
	tk, _ := store.GetTask(taskID)
	executor.Execute(ctx, *tk, task.TriggerManual)

	// Wait for the retry to happen (backoff is 1s for first retry)
	time.Sleep(3 * time.Second)

	runs, _ := store.ListRunsForTask(taskID, 10)
	if len(runs) < 2 {
		t.Errorf("expected at least 2 runs (original + retry), got %d", len(runs))
	}
}

func TestDebuggerRespectsMaxRetries(t *testing.T) {
	db := testDB(t)
	store := task.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	alwaysFail := func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", fmt.Errorf("%s", "connection refused")
	}

	taskID, _ := store.CreateTask(&task.Task{
		Name:         "always-fail",
		Prompt:       "test",
		MaxRetries:   0, // No retries
		RetryBackoff: 1,
	})

	executor := task.NewExecutor(store, alwaysFail, nil, eventBus, nil)
	debugger := NewDebugger(store, executor, eventBus, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	debugger.Start(ctx)
	defer debugger.Stop()

	tk, _ := store.GetTask(taskID)
	executor.Execute(ctx, *tk, task.TriggerManual)

	time.Sleep(2 * time.Second)

	runs, _ := store.ListRunsForTask(taskID, 10)
	// Should be 1 run only (no retries since max is 0)
	if len(runs) != 1 {
		t.Errorf("expected 1 run (no retries), got %d", len(runs))
	}
}
