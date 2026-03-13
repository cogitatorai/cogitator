package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

var (
	ErrNotFound       = errors.New("task not found")
	ErrAlreadyRunning = errors.New("task already has a run in progress")
)

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateTask(t *Task) (int64, error) {
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.ModelTier == "" {
		t.ModelTier = "cheap"
	}

	// N-1 compat: wildcard notify_users implies broadcast for older binaries.
	for _, u := range t.NotifyUsers {
		if u == "*" {
			t.Broadcast = true
			break
		}
	}

	var userID any
	if t.UserID != "" {
		userID = t.UserID
	}

	var notifyUsersJSON any
	if len(t.NotifyUsers) > 0 {
		b, err := json.Marshal(t.NotifyUsers)
		if err != nil {
			return 0, err
		}
		notifyUsersJSON = string(b)
	}

	result, err := s.db.Exec(`INSERT INTO tasks
		(name, prompt, cron_expr, model_tier, enabled, max_retries, retry_backoff,
		 timeout, working_dir, notify, allow_manual, notify_chat, broadcast,
		 notify_users, created_at, created_by, updated_at, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Prompt, t.CronExpr, t.ModelTier, t.Enabled, t.MaxRetries,
		t.RetryBackoff, t.Timeout, t.WorkingDir, t.Notify, t.AllowManual,
		t.NotifyChat, t.Broadcast, notifyUsersJSON,
		t.CreatedAt, t.CreatedBy, t.UpdatedAt, userID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.ID = id
	return id, nil
}

func (s *Store) GetTask(id int64) (*Task, error) {
	var t Task
	var cronExpr, workingDir, notify, createdBy, userID, notifyUsersRaw sql.NullString

	err := s.db.QueryRow(`SELECT id, name, prompt, cron_expr, model_tier, enabled,
		max_retries, retry_backoff, timeout, working_dir, notify, allow_manual,
		notify_chat, broadcast, notify_users, created_at, created_by, updated_at, user_id
		FROM tasks WHERE id = ?`, id).Scan(
		&t.ID, &t.Name, &t.Prompt, &cronExpr, &t.ModelTier, &t.Enabled,
		&t.MaxRetries, &t.RetryBackoff, &t.Timeout, &workingDir, &notify,
		&t.AllowManual, &t.NotifyChat, &t.Broadcast, &notifyUsersRaw,
		&t.CreatedAt, &createdBy, &t.UpdatedAt, &userID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	t.CronExpr = cronExpr.String
	t.WorkingDir = workingDir.String
	t.Notify = notify.String
	t.CreatedBy = createdBy.String
	t.UserID = userID.String
	if notifyUsersRaw.Valid {
		json.Unmarshal([]byte(notifyUsersRaw.String), &t.NotifyUsers)
	}
	// N-1 fallback: broadcast set by older binary without notify_users.
	if len(t.NotifyUsers) == 0 && t.Broadcast {
		t.NotifyUsers = []string{"*"}
	}
	return &t, nil
}

func (s *Store) UpdateTask(t *Task) error {
	t.UpdatedAt = time.Now()

	// N-1 compat: wildcard notify_users implies broadcast for older binaries.
	for _, u := range t.NotifyUsers {
		if u == "*" {
			t.Broadcast = true
			break
		}
	}

	var notifyUsersJSON any
	if len(t.NotifyUsers) > 0 {
		b, err := json.Marshal(t.NotifyUsers)
		if err != nil {
			return err
		}
		notifyUsersJSON = string(b)
	}

	_, err := s.db.Exec(`UPDATE tasks SET
		name=?, prompt=?, cron_expr=?, model_tier=?, enabled=?, max_retries=?,
		retry_backoff=?, timeout=?, working_dir=?, notify=?, allow_manual=?,
		notify_chat=?, broadcast=?, notify_users=?, updated_at=?
		WHERE id=?`,
		t.Name, t.Prompt, t.CronExpr, t.ModelTier, t.Enabled, t.MaxRetries,
		t.RetryBackoff, t.Timeout, t.WorkingDir, t.Notify, t.AllowManual,
		t.NotifyChat, t.Broadcast, notifyUsersJSON, t.UpdatedAt, t.ID)
	return err
}

func (s *Store) DeleteTask(id int64) error {
	// Remove associated runs first so they don't become orphans.
	if _, err := s.db.Exec("DELETE FROM task_runs WHERE task_id = ?", id); err != nil {
		return err
	}
	result, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListTasks(userID string) ([]Task, error) {
	query := `SELECT id, name, prompt, cron_expr, model_tier, enabled,
		max_retries, retry_backoff, timeout, working_dir, notify, allow_manual,
		notify_chat, broadcast, notify_users, created_at, created_by, updated_at, user_id
		FROM tasks`
	var args []any
	if userID != "" {
		query += ` WHERE (user_id = ? OR user_id IS NULL)`
		args = append(args, userID)
	}
	query += ` ORDER BY id ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var cronExpr, workingDir, notify, createdBy, uid, notifyUsersRaw sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Prompt, &cronExpr, &t.ModelTier,
			&t.Enabled, &t.MaxRetries, &t.RetryBackoff, &t.Timeout, &workingDir,
			&notify, &t.AllowManual, &t.NotifyChat, &t.Broadcast, &notifyUsersRaw,
			&t.CreatedAt, &createdBy, &t.UpdatedAt, &uid); err != nil {
			return nil, err
		}
		t.CronExpr = cronExpr.String
		t.WorkingDir = workingDir.String
		t.Notify = notify.String
		t.CreatedBy = createdBy.String
		t.UserID = uid.String
		if notifyUsersRaw.Valid {
			json.Unmarshal([]byte(notifyUsersRaw.String), &t.NotifyUsers)
		}
		if len(t.NotifyUsers) == 0 && t.Broadcast {
			t.NotifyUsers = []string{"*"}
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) ListScheduledTasks() ([]Task, error) {
	rows, err := s.db.Query(`SELECT id, name, prompt, cron_expr, model_tier, enabled,
		max_retries, retry_backoff, timeout, working_dir, notify, allow_manual,
		notify_chat, broadcast, notify_users, created_at, created_by, updated_at, user_id
		FROM tasks WHERE enabled = 1 AND cron_expr != '' AND cron_expr IS NOT NULL
		ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var cronExpr, workingDir, notify, createdBy, userID, notifyUsersRaw sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Prompt, &cronExpr, &t.ModelTier,
			&t.Enabled, &t.MaxRetries, &t.RetryBackoff, &t.Timeout, &workingDir,
			&notify, &t.AllowManual, &t.NotifyChat, &t.Broadcast, &notifyUsersRaw,
			&t.CreatedAt, &createdBy, &t.UpdatedAt, &userID); err != nil {
			return nil, err
		}
		t.CronExpr = cronExpr.String
		t.WorkingDir = workingDir.String
		t.Notify = notify.String
		t.CreatedBy = createdBy.String
		t.UserID = userID.String
		if notifyUsersRaw.Valid {
			json.Unmarshal([]byte(notifyUsersRaw.String), &t.NotifyUsers)
		}
		if len(t.NotifyUsers) == 0 && t.Broadcast {
			t.NotifyUsers = []string{"*"}
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// BackfillUserID assigns userID to all tasks that have a NULL or empty user_id.
// This is used at startup to fix tasks created before the auth system was added.
func (s *Store) BackfillUserID(userID string) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE tasks SET user_id = ? WHERE user_id IS NULL OR user_id = ''`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DisableAndReassignTasks disables all tasks owned by fromUserID and
// reassigns them to toUserID. Used during user deletion.
func (s *Store) DisableAndReassignTasks(fromUserID, toUserID string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET enabled = 0, user_id = ? WHERE user_id = ?`,
		toUserID, fromUserID,
	)
	return err
}

// Run CRUD

// HasRunningRun reports whether the given task already has a run in progress.
func (s *Store) HasRunningRun(taskID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM task_runs WHERE task_id = ? AND status = ?`,
		taskID, RunStatusRunning,
	).Scan(&count)
	return count > 0, err
}

func (s *Store) CreateRun(r *Run) (int64, error) {
	now := time.Now()
	r.StartedAt = now
	r.CreatedAt = now
	if r.Status == "" {
		r.Status = RunStatusRunning
	}

	result, err := s.db.Exec(`INSERT INTO task_runs
		(task_id, trigger, started_at, status, model_used, session_key,
		 parent_run_id, retry_of, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.Trigger, r.StartedAt, r.Status, r.ModelUsed,
		r.SessionKey, r.ParentRunID, r.RetryOf, r.CreatedAt)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	r.ID = id
	return id, nil
}

func (s *Store) GetRun(id int64) (*Run, error) {
	var r Run
	var taskID sql.NullInt64
	var finishedAt sql.NullTime
	var modelUsed, errorMsg, errorClass, resultSummary, transcriptPath sql.NullString
	var skillsUsed, fixApplied, sessionKey, toolCallsJSON sql.NullString
	var parentRunID, retryOf sql.NullInt64

	err := s.db.QueryRow(`SELECT id, task_id, trigger, started_at, finished_at, status,
		model_used, error_message, error_class, result_summary, transcript_path,
		skills_used, fix_applied, session_key, parent_run_id, retry_of, tool_calls, created_at
		FROM task_runs WHERE id = ?`, id).Scan(
		&r.ID, &taskID, &r.Trigger, &r.StartedAt, &finishedAt, &r.Status,
		&modelUsed, &errorMsg, &errorClass, &resultSummary, &transcriptPath,
		&skillsUsed, &fixApplied, &sessionKey, &parentRunID, &retryOf, &toolCallsJSON, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if taskID.Valid {
		r.TaskID = &taskID.Int64
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	r.ModelUsed = modelUsed.String
	r.ErrorMessage = errorMsg.String
	r.ErrorClass = errorClass.String
	r.ResultSummary = resultSummary.String
	r.TranscriptPath = transcriptPath.String
	r.SkillsUsed = skillsUsed.String
	r.FixApplied = fixApplied.String
	r.SessionKey = sessionKey.String
	if parentRunID.Valid {
		r.ParentRunID = &parentRunID.Int64
	}
	if retryOf.Valid {
		r.RetryOf = &retryOf.Int64
	}
	if toolCallsJSON.String != "" {
		json.Unmarshal([]byte(toolCallsJSON.String), &r.ToolCalls)
	}
	return &r, nil
}

func (s *Store) CompleteRun(id int64, summary string) error {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE task_runs SET
		status=?, finished_at=?, result_summary=?
		WHERE id=?`, RunStatusCompleted, now, summary, id)
	return err
}

func (s *Store) FailRun(id int64, errMsg string, errClass ErrorClass) error {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE task_runs SET
		status=?, finished_at=?, error_message=?, error_class=?
		WHERE id=?`, RunStatusFailed, now, errMsg, errClass, id)
	return err
}

func (s *Store) SetRunModel(id int64, model string) error {
	_, err := s.db.Exec(`UPDATE task_runs SET model_used=? WHERE id=?`, model, id)
	return err
}

func (s *Store) CancelRun(id int64) error {
	now := time.Now()
	_, err := s.db.Exec(`UPDATE task_runs SET status=?, finished_at=? WHERE id=?`,
		RunStatusCancelled, now, id)
	return err
}

func (s *Store) DeleteRun(id int64) error {
	result, err := s.db.Exec("DELETE FROM task_runs WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRuns removes multiple runs by ID. Returns the number of rows deleted.
func (s *Store) DeleteRuns(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]byte, 0, len(ids)*2-1)
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}
	result, err := s.db.Exec("DELETE FROM task_runs WHERE id IN ("+string(placeholders)+")", args...)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// SaveToolCalls persists the tool call records for a run as JSON.
func (s *Store) SaveToolCalls(runID int64, calls []ToolCallRecord) error {
	if len(calls) == 0 {
		return nil
	}
	data, err := json.Marshal(calls)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE task_runs SET tool_calls=? WHERE id=?`, string(data), runID)
	return err
}

// CleanupStaleRuns marks any runs left in "running" status as failed. This
// handles the case where the server was restarted while tasks were executing.
// Returns the number of runs cleaned up.
func (s *Store) CleanupStaleRuns() (int, error) {
	now := time.Now()
	result, err := s.db.Exec(`UPDATE task_runs SET
		status=?, finished_at=?, error_message=?
		WHERE status=?`,
		RunStatusFailed, now, "server restarted while task was running", RunStatusRunning)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// LastRunTime returns the most recent started_at for a given task, or the zero
// time if no runs exist.
func (s *Store) LastRunTime(taskID int64) (time.Time, error) {
	var t sql.NullTime
	err := s.db.QueryRow(
		`SELECT MAX(started_at) FROM task_runs WHERE task_id = ?`, taskID,
	).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}
	if t.Valid {
		return t.Time, nil
	}
	return time.Time{}, nil
}

func (s *Store) ListRunsForTask(taskID int64, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, task_id, trigger, started_at, finished_at, status,
		model_used, error_message, error_class, result_summary, created_at
		FROM task_runs WHERE task_id = ? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var taskID sql.NullInt64
		var finishedAt sql.NullTime
		var modelUsed, errorMsg, errorClass, resultSummary sql.NullString
		if err := rows.Scan(&r.ID, &taskID, &r.Trigger, &r.StartedAt, &finishedAt,
			&r.Status, &modelUsed, &errorMsg, &errorClass, &resultSummary, &r.CreatedAt); err != nil {
			return nil, err
		}
		if taskID.Valid {
			r.TaskID = &taskID.Int64
		}
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		r.ModelUsed = modelUsed.String
		r.ErrorMessage = errorMsg.String
		r.ErrorClass = errorClass.String
		r.ResultSummary = resultSummary.String
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) RecentRuns(limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, task_id, trigger, started_at, finished_at, status,
		model_used, error_message, error_class, result_summary, created_at
		FROM task_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var taskID sql.NullInt64
		var finishedAt sql.NullTime
		var modelUsed, errorMsg, errorClass, resultSummary sql.NullString
		if err := rows.Scan(&r.ID, &taskID, &r.Trigger, &r.StartedAt, &finishedAt,
			&r.Status, &modelUsed, &errorMsg, &errorClass, &resultSummary, &r.CreatedAt); err != nil {
			return nil, err
		}
		if taskID.Valid {
			r.TaskID = &taskID.Int64
		}
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		r.ModelUsed = modelUsed.String
		r.ErrorMessage = errorMsg.String
		r.ErrorClass = errorClass.String
		r.ResultSummary = resultSummary.String
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListRuns returns a filtered, paginated list of runs with total count.
func (s *Store) ListRuns(q RunQuery) (*RunListResult, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}

	where := "1=1"
	var args []any
	if q.Status != "" {
		where += " AND tr.status = ?"
		args = append(args, q.Status)
	}
	if q.TaskID > 0 {
		where += " AND tr.task_id = ?"
		args = append(args, q.TaskID)
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.db.QueryRow("SELECT COUNT(*) FROM task_runs tr WHERE "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, err
	}

	query := `SELECT tr.id, tr.task_id, tr.trigger, tr.started_at, tr.finished_at, tr.status,
		tr.model_used, tr.error_message, tr.error_class, tr.result_summary, tr.created_at,
		COALESCE(t.name, '') AS task_name
		FROM task_runs tr
		LEFT JOIN tasks t ON tr.task_id = t.id
		WHERE ` + where + ` ORDER BY tr.id DESC LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		var taskID sql.NullInt64
		var finishedAt sql.NullTime
		var modelUsed, errorMsg, errorClass, resultSummary, taskName sql.NullString
		if err := rows.Scan(&r.ID, &taskID, &r.Trigger, &r.StartedAt, &finishedAt,
			&r.Status, &modelUsed, &errorMsg, &errorClass, &resultSummary, &r.CreatedAt,
			&taskName); err != nil {
			return nil, err
		}
		if taskID.Valid {
			r.TaskID = &taskID.Int64
		}
		if finishedAt.Valid {
			r.FinishedAt = &finishedAt.Time
		}
		r.ModelUsed = modelUsed.String
		r.ErrorMessage = errorMsg.String
		r.ErrorClass = errorClass.String
		r.ResultSummary = resultSummary.String
		r.TaskName = taskName.String
		if r.FinishedAt != nil {
			r.DurationMs = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
		} else if r.Status == RunStatusRunning {
			r.DurationMs = time.Since(r.StartedAt).Milliseconds()
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if runs == nil {
		runs = []Run{}
	}
	return &RunListResult{Runs: runs, Total: total}, nil
}

// RunStat holds aggregated run statistics for a single task.
type RunStat struct {
	TotalRuns  int       `json:"total_runs"`
	LastStatus RunStatus `json:"last_status,omitempty"`
}

// TaskRunStats returns run count and last run status for each task in a single query.
func (s *Store) TaskRunStats() (map[int64]RunStat, error) {
	rows, err := s.db.Query(`
		SELECT task_id, COUNT(*) AS total,
			(SELECT status FROM task_runs r2
			 WHERE r2.task_id = task_runs.task_id
			 ORDER BY r2.id DESC LIMIT 1) AS last_status
		FROM task_runs
		WHERE task_id IS NOT NULL
		GROUP BY task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]RunStat)
	for rows.Next() {
		var taskID int64
		var stat RunStat
		if err := rows.Scan(&taskID, &stat.TotalRuns, &stat.LastStatus); err != nil {
			return nil, err
		}
		out[taskID] = stat
	}
	return out, rows.Err()
}

// CountActiveRuns returns the number of task runs currently in "running" status.
func (s *Store) CountActiveRuns() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM task_runs WHERE status = 'running'`).Scan(&count)
	return count, err
}
