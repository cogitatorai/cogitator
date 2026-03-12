package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

// AgentFunc runs a prompt through the full agent loop (including tool use)
// on a dedicated session and returns the final response text. The sessionKey
// identifies the conversation context; model is the resolved model name.
// The userID ties the run to a specific user so that connector tools (e.g.
// Google Calendar) can load the correct OAuth credentials.
type AgentFunc func(ctx context.Context, sessionKey, prompt, model, userID string) (string, error)

// ModelResolver maps a model tier name (e.g. "cheap", "standard") to an
// actual provider model name (e.g. "gpt-4o-mini", "gpt-5.2"). If the tier
// is unrecognized, the resolver should return the input unchanged.
type ModelResolver func(tier string) string

// Executor runs execution tasks, records results, and emits events.
type Executor struct {
	store         *Store
	agentFn       AgentFunc
	modelResolver ModelResolver
	eventBus      *bus.Bus
	logger        *slog.Logger
	mu            sync.Mutex
	activeRuns    map[int64]context.CancelFunc
}

func NewExecutor(store *Store, agentFn AgentFunc, resolver ModelResolver, eventBus *bus.Bus, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	if resolver == nil {
		resolver = func(tier string) string { return tier }
	}
	return &Executor{
		store:         store,
		agentFn:       agentFn,
		modelResolver: resolver,
		eventBus:      eventBus,
		logger:        logger,
		activeRuns:    make(map[int64]context.CancelFunc),
	}
}

// maxTaskTimeout is the hard ceiling for any task execution.
const maxTaskTimeout = 10 * time.Minute

// Execute runs a task and records the result as a task run.
func (e *Executor) Execute(ctx context.Context, t Task, trigger Trigger) (*Run, error) {
	// Prevent concurrent runs of the same task.
	if running, err := e.store.HasRunningRun(t.ID); err != nil {
		return nil, fmt.Errorf("check running: %w", err)
	} else if running {
		e.logger.Warn("task already running, skipping", "task", t.Name, "task_id", t.ID)
		return nil, ErrAlreadyRunning
	}

	run := &Run{
		TaskID:  &t.ID,
		Trigger: trigger,
	}
	runID, err := e.store.CreateRun(run)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	start := time.Now()
	log := e.logger.With("task", t.Name, "task_id", t.ID, "run_id", runID, "trigger", string(trigger))
	log.Info("task started")

	if e.eventBus != nil {
		e.eventBus.Publish(bus.Event{
			Type: bus.TaskStarted,
			Payload: map[string]any{
				"task_id": t.ID,
				"run_id":  runID,
				"trigger": string(trigger),
			},
		})
	}

	// Apply timeout: use the task's configured value, capped at maxTaskTimeout.
	timeout := maxTaskTimeout
	if t.Timeout > 0 {
		configured := time.Duration(t.Timeout) * time.Second
		if configured < timeout {
			timeout = configured
		}
	}
	var timeoutCancel context.CancelFunc
	ctx, timeoutCancel = context.WithTimeout(ctx, timeout)
	defer timeoutCancel()
	log.Info("timeout applied", "timeout", timeout.String())

	// Wrap with a cancel func so this run can be stopped externally.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	e.mu.Lock()
	e.activeRuns[runID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.activeRuns, runID)
		e.mu.Unlock()
	}()

	// Collect tool call records for this run.
	collector := &ToolCallCollector{}
	ctx = WithToolCallCollector(ctx, collector)

	// Run the task prompt through the full agent loop (with tool access).
	sessionKey := fmt.Sprintf("task:%d:run:%d", t.ID, runID)
	model := e.modelResolver(t.ModelTier)
	e.store.SetRunModel(runID, model)
	log.Info("calling agent", "model", model, "session_key", sessionKey)

	content, err := e.agentFn(ctx, sessionKey, t.Prompt, model, t.UserID)
	elapsed := time.Since(start)

	// Persist tool call records regardless of outcome.
	if records := collector.Records(); len(records) > 0 {
		if tcErr := e.store.SaveToolCalls(runID, records); tcErr != nil {
			log.Warn("failed to save tool calls", "error", tcErr)
		}
	}

	if err != nil {
		// Distinguish cancellation/timeout from other failures.
		if ctx.Err() != nil {
			reason := "cancelled"
			if ctx.Err() == context.DeadlineExceeded {
				reason = "timeout"
			}
			log.Warn("task stopped", "reason", reason, "elapsed", elapsed.String())
			e.store.CancelRun(runID)
			if e.eventBus != nil {
				e.eventBus.Publish(bus.Event{
					Type: bus.TaskCancelled,
					Payload: map[string]any{
						"task_id": t.ID,
						"run_id":  runID,
					},
				})
			}
			return e.store.GetRun(runID)
		}

		errClass := classifyError(err)
		log.Error("task failed", "error", err, "error_class", string(errClass), "elapsed", elapsed.String())
		e.store.FailRun(runID, err.Error(), errClass)
		if e.eventBus != nil {
			e.eventBus.Publish(bus.Event{
				Type: bus.TaskFailed,
				Payload: map[string]any{
					"task_id":     t.ID,
					"run_id":      runID,
					"error":       err.Error(),
					"error_class": string(errClass),
				},
			})

			if t.NotifyChat && trigger != TriggerConversation {
				e.eventBus.Publish(bus.Event{
					Type: bus.TaskNotifyChat,
					Payload: map[string]any{
						"task_id":   t.ID,
						"task_name": t.Name,
						"run_id":    runID,
						"result":    "Failed: " + err.Error(),
						"user_id":   t.UserID,
						"trigger":   string(trigger),
						"broadcast": t.Broadcast,
					},
				})
			}
		}
		return e.store.GetRun(runID)
	}

	log.Info("task completed", "elapsed", elapsed.String(), "result_len", len(content))
	e.store.CompleteRun(runID, content)

	// Post-run self-healing: if the run had failures, spawn a capable agent
	// to diagnose and fix the root cause (skill, prompt, or transient).
	e.selfHeal(ctx, t, runID, collector, e.modelResolver("standard"), "")

	if e.eventBus != nil {
		e.eventBus.Publish(bus.Event{
			Type: bus.TaskCompleted,
			Payload: map[string]any{
				"task_id": t.ID,
				"run_id":  runID,
			},
		})

		if t.NotifyChat && trigger != TriggerConversation {
			e.eventBus.Publish(bus.Event{
				Type: bus.TaskNotifyChat,
				Payload: map[string]any{
					"task_id":   t.ID,
					"task_name": t.Name,
					"run_id":    runID,
					"result":    content,
					"user_id":   t.UserID,
					"trigger":   string(trigger),
					"broadcast": t.Broadcast,
				},
			})
		}
	}

	return e.store.GetRun(runID)
}

// Cancel stops a running task by cancelling its context. Returns true if the
// run was actively running in this process and was cancelled.
func (e *Executor) Cancel(runID int64) bool {
	e.mu.Lock()
	cancel, ok := e.activeRuns[runID]
	if ok {
		delete(e.activeRuns, runID)
	}
	e.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// selfHeal inspects the tool call log from a completed run. If there were
// failures (or if reason is non-empty), it spawns a capable agent to diagnose
// and fix the root cause, whether that is a broken skill, an incorrect task
// prompt, or a transient issue. When reason is provided, the HasFailures
// check is skipped because the caller already knows something is wrong (e.g.
// the output was empty or incorrect). This is best-effort; errors are logged
// but never propagated.
func (e *Executor) selfHeal(ctx context.Context, t Task, runID int64, collector *ToolCallCollector, model, reason string) {
	if reason == "" && !collector.HasFailures() {
		return
	}

	// Find the read_skill call and extract the node_id (if any).
	var nodeID string
	for _, r := range collector.Records() {
		if r.Tool == "read_skill" && r.Arguments != "" {
			var args struct {
				NodeID string `json:"node_id"`
			}
			if err := json.Unmarshal([]byte(r.Arguments), &args); err == nil && args.NodeID != "" {
				nodeID = args.NodeID
				break
			}
		}
	}

	// Build a human-readable tool call log with truncated results.
	var log strings.Builder
	for _, r := range collector.Records() {
		status := "OK"
		if r.Error != "" {
			status = "FAILED: " + r.Error
		}
		// Truncate arguments for readability.
		args := r.Arguments
		if len(args) > 120 {
			args = args[:120] + "..."
		}
		fmt.Fprintf(&log, "Round %d: %s(%s) -> %s\n", r.Round, r.Tool, args, status)
		// Include truncated result when available.
		res := r.Result
		if len(res) > 300 {
			res = res[:300] + "..."
		}
		if res != "" {
			fmt.Fprintf(&log, "  Result: %s\n", res)
		}
	}

	var skillLine string
	if nodeID != "" {
		skillLine = fmt.Sprintf("Skill node_id: %s\n\n", nodeID)
	}

	// When a reason is provided, fetch the last run's result summary
	// so the healing agent can see what the task actually produced.
	var reasonBlock string
	if reason != "" {
		reasonBlock = fmt.Sprintf("Reason for healing: %s\n\n", reason)
		if run, err := e.store.GetRun(runID); err == nil && run.ResultSummary != "" {
			reasonBlock += fmt.Sprintf("Last run result:\n%s\n\n", run.ResultSummary)
		}
	}

	prompt := fmt.Sprintf(`[SELF-HEALING] A task run encountered issues. Diagnose the root cause
and fix it so future runs succeed.

Task: %s (ID: %d)
Schedule: %s
Model tier: %s

Task prompt:
%s

%s%sTool call log (with results):
%s
Instructions:
1. Analyze the failures and determine the root cause.
2. If this is transient (network down, API rate-limited), do nothing.
3. If the skill needs fixing: call read_skill, then update_skill with
   corrected content.
4. If the task prompt needs fixing: call update_task with the task ID
   and only the fields that need changing (e.g. prompt). Do NOT delete
   and recreate the task.
5. You may fix both. Fix ONLY what caused the failures.
6. NEVER work around security restrictions. If a command was blocked
   (sensitive path, dangerous command, disallowed domain), that is
   intentional. Fix the skill/prompt to use allowed alternatives, or
   leave it unchanged if no safe alternative exists.
`, t.Name, t.ID, t.CronExpr, t.ModelTier, t.Prompt, reasonBlock, skillLine, log.String())

	sessionKey := fmt.Sprintf("task:%d:run:%d:self-heal", t.ID, runID)

	e.logger.Info("spawning self-healing agent",
		"task_id", t.ID,
		"run_id", runID,
		"has_skill", nodeID != "",
		"model", model,
	)

	if _, err := e.agentFn(ctx, sessionKey, prompt, model, t.UserID); err != nil {
		e.logger.Warn("self-healing failed", "error", err, "task_id", t.ID, "run_id", runID)
	}
}

// HealTask triggers self-healing for a task based on its most recent run.
// If reason is non-empty, healing runs even when the run had no tool call
// failures, which handles cases where the task "succeeded" but produced
// wrong output (empty digests, stale data, etc.).
func (e *Executor) HealTask(ctx context.Context, taskID int64, reason string) (string, error) {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}

	runs, err := e.store.ListRunsForTask(taskID, 1)
	if err != nil {
		return "", fmt.Errorf("failed to list runs: %w", err)
	}
	if len(runs) == 0 {
		return "", fmt.Errorf("task %d has no runs to heal", taskID)
	}

	lastRun := runs[0]

	// GetRun includes the full tool_calls JSON; ListRunsForTask does not.
	full, err := e.store.GetRun(lastRun.ID)
	if err != nil {
		return "", fmt.Errorf("failed to load run %d: %w", lastRun.ID, err)
	}

	collector := NewCollectorFromRecords(full.ToolCalls)

	// Default reason when the run had failures but no explicit reason given.
	if reason == "" && collector.HasFailures() {
		reason = "automatic: tool call failures detected"
	}
	if reason == "" {
		return "run has no failures and no reason provided; nothing to heal", nil
	}

	model := e.modelResolver("standard")
	e.selfHeal(ctx, *t, full.ID, collector, model, reason)

	return fmt.Sprintf("healing initiated for task %d (run %d): %s", taskID, full.ID, reason), nil
}

// classifyError determines whether an error is transient or systemic.
func classifyError(err error) ErrorClass {
	if ctx_err := context.Cause(context.Background()); ctx_err != nil {
		return ErrorTransient
	}
	msg := err.Error()
	for _, keyword := range []string{"timeout", "deadline", "connection refused", "eof", "temporary", "unavailable"} {
		if containsLower(msg, keyword) {
			return ErrorTransient
		}
	}
	return ErrorSystemic
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 32
			}
			if c != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
