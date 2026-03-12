package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

func (r *Router) handleListTasks(w http.ResponseWriter, req *http.Request) {
	tasks, err := r.tasks.ListTasks(userIDFromRequest(req))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	if tasks == nil {
		tasks = []task.Task{}
	}
	r.enrichTasks(tasks)
	writeJSON(w, http.StatusOK, tasks)
}

// enrichTasks populates transient fields (next run, run stats) on tasks.
func (r *Router) enrichTasks(tasks []task.Task) {
	if r.scheduler != nil {
		times := r.scheduler.NextRunTimes()
		for i := range tasks {
			if t, ok := times[tasks[i].ID]; ok {
				tasks[i].NextRunAt = &t
			}
		}
	}

	for i := range tasks {
		if tasks[i].CronExpr != "" {
			tasks[i].CronDescription = task.DescribeCron(tasks[i].CronExpr)
		}
	}

	if stats, err := r.tasks.TaskRunStats(); err == nil {
		for i := range tasks {
			if s, ok := stats[tasks[i].ID]; ok {
				tasks[i].TotalRuns = s.TotalRuns
				tasks[i].LastStatus = s.LastStatus
			}
		}
	}
}

func (r *Router) handleCreateTask(w http.ResponseWriter, req *http.Request) {
	var t task.Task
	if err := json.NewDecoder(req.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if t.Name == "" || t.Prompt == "" {
		writeError(w, http.StatusBadRequest, "name and prompt are required")
		return
	}

	t.UserID = userIDFromRequest(req)

	id, err := r.tasks.CreateTask(&t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}

	created, _ := r.tasks.GetTask(id)
	writeJSON(w, http.StatusCreated, created)
}

func (r *Router) handleGetTask(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	t, err := r.tasks.GetTask(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if !ownsResource(w, req, t.UserID) {
		return
	}
	slice := []task.Task{*t}
	r.enrichTasks(slice)
	*t = slice[0]
	writeJSON(w, http.StatusOK, t)
}

func (r *Router) handleUpdateTask(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	existing, err := r.tasks.GetTask(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if !ownsResource(w, req, existing.UserID) {
		return
	}

	if err := json.NewDecoder(req.Body).Decode(existing); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	existing.ID = id

	if err := r.tasks.UpdateTask(existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update task")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}

	updated, _ := r.tasks.GetTask(id)
	writeJSON(w, http.StatusOK, updated)
}

func (r *Router) handleDeleteTask(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	t, err := r.tasks.GetTask(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if !ownsResource(w, req, t.UserID) {
		return
	}

	if err := r.tasks.DeleteTask(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete task")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.TaskChanged})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleTriggerTask(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	t, err := r.tasks.GetTask(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if !ownsResource(w, req, t.UserID) {
		return
	}
	if !t.AllowManual {
		writeError(w, http.StatusForbidden, "task does not allow manual triggering")
		return
	}

	if r.taskExecutor == nil {
		writeError(w, http.StatusServiceUnavailable, "task execution is not configured")
		return
	}

	// Reject if this task already has a run in progress.
	if running, err := r.tasks.HasRunningRun(t.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check running state")
		return
	} else if running {
		writeError(w, http.StatusConflict, "task already has a run in progress")
		return
	}

	// Execute asynchronously. The executor creates the run record, runs the
	// agent, logs everything, and persists results.
	go r.taskExecutor.Execute(context.Background(), *t, task.TriggerManual)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_id": t.ID,
		"status":  "accepted",
	})
}

func (r *Router) handleListTaskRuns(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task ID")
		return
	}

	// Verify task ownership.
	t, err := r.tasks.GetTask(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if !ownsResource(w, req, t.UserID) {
		return
	}

	limit := 50
	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	runs, err := r.tasks.ListRunsForTask(id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	if runs == nil {
		runs = []task.Run{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (r *Router) handleGetRun(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("run_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := r.tasks.GetRun(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	if !r.ownsRun(w, req, run) {
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (r *Router) handleRecentRuns(w http.ResponseWriter, req *http.Request) {
	limit := 20
	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	runs, err := r.tasks.RecentRuns(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	// Filter to only runs belonging to caller's tasks (admin sees all).
	runs = r.filterRunsByOwner(req, runs)
	if runs == nil {
		runs = []task.Run{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (r *Router) handleListRuns(w http.ResponseWriter, req *http.Request) {
	q := task.RunQuery{Limit: 50}

	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			q.Limit = parsed
		}
	}
	if o := req.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			q.Offset = parsed
		}
	}
	q.Status = req.URL.Query().Get("status")
	if tid := req.URL.Query().Get("task_id"); tid != "" {
		if parsed, err := strconv.ParseInt(tid, 10, 64); err == nil {
			q.TaskID = parsed
		}
	}

	// If a specific task_id is given, verify the caller owns that task.
	if q.TaskID != 0 {
		t, err := r.tasks.GetTask(q.TaskID)
		if err == task.ErrNotFound {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if !ownsResource(w, req, t.UserID) {
			return
		}
	}

	result, err := r.tasks.ListRuns(q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleCancelRun(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := r.tasks.GetRun(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	if !r.ownsRun(w, req, run) {
		return
	}
	if run.Status != task.RunStatusRunning {
		writeError(w, http.StatusConflict, "run is not running")
		return
	}

	// Try to cancel via the executor (active in this process).
	if r.taskExecutor != nil && r.taskExecutor.Cancel(id) {
		// The executor's goroutine will mark it as cancelled via the context
		// cancellation path. Give it a moment, then return the updated run.
		// The status update happens asynchronously so fetch fresh state.
	} else {
		// Orphaned run (server restarted, or triggered externally). Cancel directly.
		if err := r.tasks.CancelRun(id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to cancel run")
			return
		}
	}

	updated, _ := r.tasks.GetRun(id)
	writeJSON(w, http.StatusOK, updated)
}

func (r *Router) handleDeleteRun(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := r.tasks.GetRun(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if !r.ownsRun(w, req, run) {
		return
	}

	if err := r.tasks.DeleteRun(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete run")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleDeleteRuns(w http.ResponseWriter, req *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids array is required")
		return
	}

	// Verify ownership of each run's parent task before deleting.
	for _, rid := range body.IDs {
		run, err := r.tasks.GetRun(rid)
		if err != nil {
			continue // skip missing runs
		}
		if !r.ownsRun(w, req, run) {
			return
		}
	}

	deleted, err := r.tasks.DeleteRuns(body.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete runs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}

func (r *Router) handleGetRunByID(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := r.tasks.GetRun(id)
	if err == task.ErrNotFound {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get run")
		return
	}
	if !r.ownsRun(w, req, run) {
		return
	}

	// Compute duration.
	if run.FinishedAt != nil {
		run.DurationMs = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
	}

	// Resolve task name.
	if run.TaskID != nil {
		if t, err := r.tasks.GetTask(*run.TaskID); err == nil {
			run.TaskName = t.Name
		}
	}

	writeJSON(w, http.StatusOK, run)
}

// ownsRun checks that the caller owns the task associated with a run.
// Returns true if authorized; writes 404 and returns false otherwise.
func (r *Router) ownsRun(w http.ResponseWriter, req *http.Request, run *task.Run) bool {
	uid := userIDFromRequest(req)
	if uid == "" || isAdmin(req) {
		return true
	}
	if run.TaskID == nil {
		return true // ad-hoc run with no parent task
	}
	t, err := r.tasks.GetTask(*run.TaskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	if t.UserID != uid {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

// filterRunsByOwner filters a slice of runs to only those belonging to the caller's tasks.
// Admin users see all runs. When no auth context exists, all runs pass through.
func (r *Router) filterRunsByOwner(req *http.Request, runs []task.Run) []task.Run {
	uid := userIDFromRequest(req)
	if uid == "" || isAdmin(req) {
		return runs
	}
	// Build a set of task IDs owned by this user.
	userTasks, _ := r.tasks.ListTasks(uid)
	owned := make(map[int64]bool, len(userTasks))
	for _, t := range userTasks {
		owned[t.ID] = true
	}
	filtered := make([]task.Run, 0, len(runs))
	for _, run := range runs {
		if run.TaskID == nil || owned[*run.TaskID] {
			filtered = append(filtered, run)
		}
	}
	return filtered
}
