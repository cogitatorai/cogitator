package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

// Debugger subscribes to task failures and determines the appropriate response.
// Transient failures are retried with backoff. Systemic failures are logged
// for analysis (a future LLM-driven diagnosis step will be added).
type Debugger struct {
	store    *task.Store
	executor *task.Executor
	eventBus *bus.Bus
	logger   *slog.Logger
	cancel   context.CancelFunc
}

func NewDebugger(store *task.Store, executor *task.Executor, eventBus *bus.Bus, logger *slog.Logger) *Debugger {
	if logger == nil {
		logger = slog.Default()
	}
	return &Debugger{
		store:    store,
		executor: executor,
		eventBus: eventBus,
		logger:   logger,
	}
}

func (d *Debugger) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	ch := d.eventBus.Subscribe(bus.TaskFailed)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-ch:
				d.handleFailure(ctx, evt)
			}
		}
	}()

	d.logger.Info("debugger started")
}

func (d *Debugger) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *Debugger) handleFailure(ctx context.Context, evt bus.Event) {
	runID, ok := evt.Payload["run_id"].(int64)
	if !ok {
		return
	}
	taskID, ok := evt.Payload["task_id"].(int64)
	if !ok {
		return
	}
	errorClass, _ := evt.Payload["error_class"].(string)

	d.logger.Info("handling task failure",
		"run_id", runID,
		"task_id", taskID,
		"error_class", errorClass,
	)

	if errorClass == string(task.ErrorTransient) {
		d.handleTransient(ctx, taskID, runID)
	} else {
		d.handleSystemic(taskID, runID, evt)
	}
}

func (d *Debugger) handleTransient(ctx context.Context, taskID int64, failedRunID int64) {
	tk, err := d.store.GetTask(taskID)
	if err != nil {
		d.logger.Error("failed to get task for retry", "task_id", taskID, "error", err)
		return
	}

	// Check how many retries have been attempted
	runs, _ := d.store.ListRunsForTask(taskID, 50)
	retryCount := 0
	for _, r := range runs {
		if r.Status == task.RunStatusFailed && r.ErrorClass == string(task.ErrorTransient) {
			retryCount++
		}
	}

	if retryCount >= tk.MaxRetries {
		d.logger.Warn("max retries exceeded, not retrying",
			"task_id", taskID,
			"retries", retryCount,
			"max", tk.MaxRetries,
		)
		return
	}

	// Backoff before retry
	backoff := time.Duration(tk.RetryBackoff) * time.Second * time.Duration(retryCount+1)
	d.logger.Info("scheduling retry",
		"task_id", taskID,
		"retry", retryCount+1,
		"backoff", backoff,
	)

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			run := &task.Run{
				TaskID:  &taskID,
				Trigger: task.TriggerSystem,
				RetryOf: &failedRunID,
			}
			d.store.CreateRun(run)
			d.executor.Execute(ctx, *tk, task.TriggerSystem)
		}
	}()
}

func (d *Debugger) handleSystemic(taskID int64, runID int64, evt bus.Event) {
	errorMsg, _ := evt.Payload["error"].(string)
	d.logger.Warn("systemic failure detected, needs investigation",
		"task_id", taskID,
		"run_id", runID,
		"error", errorMsg,
	)
	// Future: invoke LLM to diagnose and suggest fixes
}
