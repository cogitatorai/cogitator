package task

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

func mockAgentSuccess(content string) AgentFunc {
	return func(_ context.Context, _, _, _, _ string) (string, error) {
		return content, nil
	}
}

func mockAgentError(msg string) AgentFunc {
	return func(_ context.Context, _, _, _, _ string) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

func TestExecutorSuccess(t *testing.T) {
	store := NewStore(testDB(t))
	eventBus := bus.New()
	defer eventBus.Close()

	completed := eventBus.Subscribe(bus.TaskCompleted)

	taskID, _ := store.CreateTask(&Task{
		Name:   "test task",
		Prompt: "Do something",
	})
	tk, _ := store.GetTask(taskID)

	executor := NewExecutor(store, mockAgentSuccess("Task done!"), nil, eventBus, nil)
	run, err := executor.Execute(context.Background(), *tk, TriggerManual)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if run.Status != RunStatusCompleted {
		t.Errorf("expected 'completed', got %q", run.Status)
	}
	if run.ResultSummary != "Task done!" {
		t.Errorf("expected 'Task done!', got %q", run.ResultSummary)
	}

	select {
	case evt := <-completed:
		if evt.Payload["task_id"] != taskID {
			t.Errorf("expected task_id %d, got %v", taskID, evt.Payload["task_id"])
		}
	default:
		t.Error("expected TaskCompleted event")
	}
}

func TestExecutorFailure(t *testing.T) {
	store := NewStore(testDB(t))
	eventBus := bus.New()
	defer eventBus.Close()

	failed := eventBus.Subscribe(bus.TaskFailed)

	taskID, _ := store.CreateTask(&Task{
		Name:   "failing task",
		Prompt: "Will fail",
	})
	tk, _ := store.GetTask(taskID)

	executor := NewExecutor(store, mockAgentError("connection refused"), nil, eventBus, nil)
	run, err := executor.Execute(context.Background(), *tk, TriggerCron)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if run.Status != RunStatusFailed {
		t.Errorf("expected 'failed', got %q", run.Status)
	}
	if run.ErrorClass != string(ErrorTransient) {
		t.Errorf("expected 'transient' (connection refused), got %q", run.ErrorClass)
	}

	select {
	case evt := <-failed:
		if evt.Payload["error_class"] != string(ErrorTransient) {
			t.Errorf("expected transient error class")
		}
	default:
		t.Error("expected TaskFailed event")
	}
}

func TestExecutorSystemicError(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "systemic", Prompt: "fail"})
	tk, _ := store.GetTask(taskID)

	executor := NewExecutor(store, mockAgentError("invalid prompt format"), nil, nil, nil)
	run, _ := executor.Execute(context.Background(), *tk, TriggerManual)

	if run.ErrorClass != string(ErrorSystemic) {
		t.Errorf("expected 'systemic', got %q", run.ErrorClass)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err      string
		expected ErrorClass
	}{
		{"connection refused", ErrorTransient},
		{"timeout exceeded", ErrorTransient},
		{"deadline exceeded", ErrorTransient},
		{"unexpected EOF", ErrorTransient},
		{"service unavailable", ErrorTransient},
		{"invalid prompt", ErrorSystemic},
		{"parsing error", ErrorSystemic},
	}

	for _, tc := range tests {
		got := classifyError(fmt.Errorf("%s", tc.err))
		if got != tc.expected {
			t.Errorf("classifyError(%q) = %q, want %q", tc.err, got, tc.expected)
		}
	}
}

func TestSelfHealCalledOnFailures(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "skill task", Prompt: "use skill", CronExpr: "0 * * * *", ModelTier: "standard"})
	tk, _ := store.GetTask(taskID)

	// Track whether the self-healing agent call was made and what it received.
	var healCalls []struct{ session, prompt, model string }
	agent := func(_ context.Context, session, prompt, model, _ string) (string, error) {
		healCalls = append(healCalls, struct{ session, prompt, model string }{session, prompt, model})
		return "healed", nil
	}

	resolver := func(tier string) string {
		if tier == "cheap" {
			return "cheap-model"
		}
		return "standard-model"
	}

	executor := NewExecutor(store, agent, resolver, nil, nil)

	// Seed the collector with a skill read and a failure.
	collector := &ToolCallCollector{}
	collector.Record("read_skill", `{"node_id":"skill-abc123"}`, "# Skill content...", time.Second, 0, nil)
	collector.Record("shell", `{"command":"export FOO=bar"}`, "", time.Second, 1, fmt.Errorf("blocked: export not allowed"))
	collector.Record("shell", `{"command":"curl http://example.com"}`, "(empty)", time.Second, 1, nil)

	// Call selfHeal directly (it's what Execute calls after completion).
	run, _ := store.CreateRun(&Run{TaskID: &tk.ID, Trigger: TriggerManual})
	executor.selfHeal(context.Background(), *tk, run, collector, resolver("standard"), "")

	if len(healCalls) != 1 {
		t.Fatalf("expected 1 self-heal call, got %d", len(healCalls))
	}

	call := healCalls[0]
	if !strings.Contains(call.session, "self-heal") {
		t.Errorf("expected session key containing 'self-heal', got %q", call.session)
	}
	if !strings.Contains(call.prompt, "skill-abc123") {
		t.Errorf("expected prompt containing node_id 'skill-abc123', got %q", call.prompt)
	}
	if !strings.Contains(call.prompt, "FAILED") {
		t.Errorf("expected prompt containing failure details")
	}
	if !strings.Contains(call.prompt, "use skill") {
		t.Errorf("expected prompt containing task prompt")
	}
	if call.model != "standard-model" {
		t.Errorf("expected standard-model, got %q", call.model)
	}
}

func TestSelfHealSkippedOnCleanRun(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "clean task", Prompt: "no failures"})
	tk, _ := store.GetTask(taskID)

	called := false
	agent := func(_ context.Context, _, _, _, _ string) (string, error) {
		called = true
		return "done", nil
	}

	executor := NewExecutor(store, agent, nil, nil, nil)

	// All successful records, including a read_skill.
	collector := &ToolCallCollector{}
	collector.Record("read_skill", `{"node_id":"skill-xyz"}`, "skill content", time.Second, 0, nil)
	collector.Record("shell", `{"command":"echo hello"}`, "hello", time.Second, 1, nil)

	called = false // reset
	run, _ := store.CreateRun(&Run{TaskID: &tk.ID, Trigger: TriggerManual})
	executor.selfHeal(context.Background(), *tk, run, collector, "standard-model", "")

	if called {
		t.Error("expected selfHeal to NOT call agentFn on clean run")
	}
}

func TestSelfHealCalledWithoutSkill(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "no skill", Prompt: "fails without skill"})
	tk, _ := store.GetTask(taskID)

	var healCalls []struct{ session, prompt, model string }
	agent := func(_ context.Context, session, prompt, model, _ string) (string, error) {
		healCalls = append(healCalls, struct{ session, prompt, model string }{session, prompt, model})
		return "done", nil
	}

	executor := NewExecutor(store, agent, nil, nil, nil)

	// Failures but no read_skill call: self-healing should still run.
	collector := &ToolCallCollector{}
	collector.Record("shell", `{"command":"export BAD"}`, "", time.Second, 0, fmt.Errorf("blocked"))

	run, _ := store.CreateRun(&Run{TaskID: &tk.ID, Trigger: TriggerManual})
	executor.selfHeal(context.Background(), *tk, run, collector, "standard-model", "")

	if len(healCalls) != 1 {
		t.Fatalf("expected 1 self-heal call (prompt-only fix), got %d", len(healCalls))
	}

	call := healCalls[0]
	if strings.Contains(call.prompt, "Skill node_id") {
		t.Error("expected prompt to NOT contain skill node_id when no skill was read")
	}
	if !strings.Contains(call.prompt, "FAILED") {
		t.Error("expected prompt containing failure details")
	}
	if !strings.Contains(call.prompt, "fails without skill") {
		t.Error("expected prompt containing task prompt")
	}
}

func TestHealTask(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "heal-me", Prompt: "do stuff", CronExpr: "0 * * * *", ModelTier: "cheap"})
	tk, _ := store.GetTask(taskID)

	// Create a run with tool call failures.
	runID, _ := store.CreateRun(&Run{TaskID: &tk.ID, Trigger: TriggerCron})
	store.SaveToolCalls(runID, []ToolCallRecord{
		{Tool: "shell", Arguments: `{"command":"bad"}`, Error: "blocked", Round: 0},
	})
	store.CompleteRun(runID, "partial result")

	var healCalls []struct{ session, prompt, model string }
	agent := func(_ context.Context, session, prompt, model, _ string) (string, error) {
		healCalls = append(healCalls, struct{ session, prompt, model string }{session, prompt, model})
		return "healed", nil
	}

	resolver := func(tier string) string {
		if tier == "standard" {
			return "standard-model"
		}
		return "cheap-model"
	}

	executor := NewExecutor(store, agent, resolver, nil, nil)

	result, err := executor.HealTask(context.Background(), taskID, "")
	if err != nil {
		t.Fatalf("HealTask() error: %v", err)
	}
	if !strings.Contains(result, "healing initiated") {
		t.Errorf("expected confirmation message, got %q", result)
	}

	if len(healCalls) != 1 {
		t.Fatalf("expected 1 self-heal call, got %d", len(healCalls))
	}
	call := healCalls[0]
	if call.model != "standard-model" {
		t.Errorf("expected standard-model, got %q", call.model)
	}
	if !strings.Contains(call.prompt, "FAILED") {
		t.Error("expected prompt containing failure details")
	}
}

func TestHealTaskWithReason(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "bad-output", Prompt: "fetch digest", CronExpr: "0 9 * * *", ModelTier: "cheap"})
	tk, _ := store.GetTask(taskID)

	// Create a run that succeeded (no tool call errors) but produced bad output.
	runID, _ := store.CreateRun(&Run{TaskID: &tk.ID, Trigger: TriggerCron})
	store.SaveToolCalls(runID, []ToolCallRecord{
		{Tool: "shell", Arguments: `{"command":"curl api"}`, Result: "(empty)", Round: 0},
	})
	store.CompleteRun(runID, "")

	var healCalls []struct{ session, prompt, model string }
	agent := func(_ context.Context, session, prompt, model, _ string) (string, error) {
		healCalls = append(healCalls, struct{ session, prompt, model string }{session, prompt, model})
		return "fixed", nil
	}

	executor := NewExecutor(store, agent, func(string) string { return "std" }, nil, nil)

	result, err := executor.HealTask(context.Background(), taskID, "the digest was empty")
	if err != nil {
		t.Fatalf("HealTask() error: %v", err)
	}
	if !strings.Contains(result, "the digest was empty") {
		t.Errorf("expected reason in confirmation, got %q", result)
	}

	if len(healCalls) != 1 {
		t.Fatalf("expected 1 self-heal call, got %d", len(healCalls))
	}
	call := healCalls[0]
	if !strings.Contains(call.prompt, "Reason for healing: the digest was empty") {
		t.Errorf("expected prompt to contain reason, got %q", call.prompt)
	}
	if !strings.Contains(call.prompt, "fetch digest") {
		t.Error("expected prompt to contain task prompt")
	}
}

func TestHealTaskNoRuns(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "no-runs", Prompt: "never ran"})

	executor := NewExecutor(store, mockAgentSuccess("ok"), nil, nil, nil)

	_, err := executor.HealTask(context.Background(), taskID, "something wrong")
	if err == nil {
		t.Fatal("expected error for task with no runs")
	}
	if !strings.Contains(err.Error(), "no runs") {
		t.Errorf("expected 'no runs' error, got %q", err.Error())
	}
}
