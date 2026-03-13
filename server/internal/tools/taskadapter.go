package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

// TaskStoreAdapter adapts task.Store and task.Executor to the TaskCreator
// interface so the tools executor can create, list, and run tasks without
// depending on the full task package API.
// TaskScheduler exposes next-run information from the cron scheduler.
type TaskScheduler interface {
	NextRunTimes() map[int64]time.Time
}

type TaskStoreAdapter struct {
	Store      *task.Store
	Executor   *task.Executor
	EventBus   *bus.Bus
	Scheduler  TaskScheduler
	UserLister UserLister // for resolving notify_users IDs to names
}

func (a *TaskStoreAdapter) CreateTask(name, prompt, cronExpr, modelTier string, notifyChat bool, userID string, notifyUsers []string) (int64, error) {
	t := &task.Task{
		Name:        name,
		Prompt:      prompt,
		CronExpr:    cronExpr,
		ModelTier:   modelTier,
		NotifyChat:  notifyChat,
		NotifyUsers: notifyUsers,
		Enabled:     true,
		AllowManual: true,
		CreatedBy:   "agent",
		UserID:      userID,
	}
	id, err := a.Store.CreateTask(t)
	if err == nil && a.EventBus != nil {
		a.EventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}
	return id, err
}

func (a *TaskStoreAdapter) UpdateTask(id int64, prompt, cronExpr, modelTier *string, notifyChat *bool, notifyUsers *[]string) error {
	t, err := a.Store.GetTask(id)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if prompt != nil {
		t.Prompt = *prompt
	}
	if cronExpr != nil {
		t.CronExpr = *cronExpr
	}
	if modelTier != nil {
		t.ModelTier = *modelTier
	}
	if notifyChat != nil {
		t.NotifyChat = *notifyChat
	}
	if notifyUsers != nil {
		t.NotifyUsers = *notifyUsers
	}
	if err := a.Store.UpdateTask(t); err != nil {
		return err
	}
	if a.EventBus != nil {
		a.EventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}
	return nil
}

func (a *TaskStoreAdapter) ListTasks(userID string) ([]map[string]any, error) {
	tasks, err := a.Store.ListTasks(userID)
	if err != nil {
		return nil, err
	}

	var nextRuns map[int64]time.Time
	if a.Scheduler != nil {
		nextRuns = a.Scheduler.NextRunTimes()
	}

	result := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		entry := map[string]any{
			"id":         t.ID,
			"name":       t.Name,
			"schedule":   task.DescribeCron(t.CronExpr),
			"enabled":    t.Enabled,
			"model_tier": t.ModelTier,
			"prompt":     t.Prompt,
		}
		if next, ok := nextRuns[t.ID]; ok {
			entry["next_run"] = task.FormatNextRun(next)
		}
		if len(t.NotifyUsers) > 0 {
			if len(t.NotifyUsers) == 1 && t.NotifyUsers[0] == "*" {
				entry["notify_users"] = []string{"everyone"}
			} else if a.UserLister != nil {
				allUsers, _ := a.UserLister.ListAllUsers()
				idToName := make(map[string]string, len(allUsers))
				for _, u := range allUsers {
					idToName[u.ID] = u.Name
				}
				names := make([]string, 0, len(t.NotifyUsers))
				for _, uid := range t.NotifyUsers {
					if name, ok := idToName[uid]; ok {
						names = append(names, name)
					}
				}
				entry["notify_users"] = names
			}
		}
		result[i] = entry
	}
	return result, nil
}

func (a *TaskStoreAdapter) RunTask(ctx context.Context, id int64) (map[string]any, error) {
	if a.Executor == nil {
		return nil, fmt.Errorf("task execution is not configured")
	}

	t, err := a.Store.GetTask(id)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	run, err := a.Executor.Execute(ctx, *t, task.TriggerConversation)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"run_id":  run.ID,
		"task_id": id,
		"task":    t.Name,
		"status":  string(run.Status),
	}
	if run.ResultSummary != "" {
		result["result"] = run.ResultSummary
	}
	if run.ErrorMessage != "" {
		result["error"] = run.ErrorMessage
	}
	return result, nil
}

func (a *TaskStoreAdapter) ToggleTask(id int64, enabled bool) error {
	t, err := a.Store.GetTask(id)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	t.Enabled = enabled
	if err := a.Store.UpdateTask(t); err != nil {
		return err
	}
	if a.EventBus != nil {
		a.EventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}
	return nil
}

func (a *TaskStoreAdapter) DeleteTask(id int64) error {
	err := a.Store.DeleteTask(id)
	if err == nil && a.EventBus != nil {
		a.EventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}
	return err
}

func (a *TaskStoreAdapter) HealTask(ctx context.Context, id int64, reason string) (string, error) {
	if a.Executor == nil {
		return "", fmt.Errorf("task execution is not configured")
	}
	return a.Executor.HealTask(ctx, id, reason)
}
