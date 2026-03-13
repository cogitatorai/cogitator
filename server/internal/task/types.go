package task

import "time"

type Trigger string

const (
	TriggerCron         Trigger = "cron"
	TriggerManual       Trigger = "manual"
	TriggerConversation Trigger = "conversation"
	TriggerSpawned      Trigger = "spawned"
	TriggerSystem       Trigger = "system"
	TriggerCatchUp      Trigger = "catchup"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusRetrying   RunStatus = "retrying"
	RunStatusCancelled  RunStatus = "cancelled"
)

type ErrorClass string

const (
	ErrorTransient ErrorClass = "transient"
	ErrorSystemic  ErrorClass = "systemic"
)

type Task struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Prompt       string    `json:"prompt"`
	CronExpr     string    `json:"cron_expr,omitempty"`
	ModelTier    string    `json:"model_tier"`
	Enabled      bool      `json:"enabled"`
	UserID       string    `json:"user_id,omitempty"`
	MaxRetries   int       `json:"max_retries"`
	RetryBackoff int       `json:"retry_backoff"`
	Timeout      int       `json:"timeout"`
	WorkingDir   string    `json:"working_dir,omitempty"`
	Notify       string    `json:"notify,omitempty"`
	AllowManual  bool      `json:"allow_manual"`
	NotifyChat   bool      `json:"notify_chat"`
	Broadcast    bool      `json:"broadcast"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Transient fields populated at query time; not persisted.
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	CronDescription string     `json:"cron_description,omitempty"`
	TotalRuns  int        `json:"total_runs"`
	LastStatus RunStatus  `json:"last_status,omitempty"`
	OwnerName  string     `json:"owner_name,omitempty"`
}

// RunQuery specifies filters and pagination for listing runs.
type RunQuery struct {
	Status string
	TaskID int64 // 0 means all tasks
	Limit  int
	Offset int
}

// RunListResult holds a page of runs and the total matching count.
type RunListResult struct {
	Runs  []Run `json:"runs"`
	Total int   `json:"total"`
}

type Run struct {
	ID              int64      `json:"id"`
	TaskID          *int64     `json:"task_id,omitempty"`
	TaskName        string     `json:"task_name,omitempty"`
	Trigger         Trigger    `json:"trigger"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	DurationMs      int64      `json:"duration_ms"`
	Status          RunStatus  `json:"status"`
	ModelUsed       string     `json:"model_used,omitempty"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	ErrorClass      string     `json:"error_class,omitempty"`
	ResultSummary   string     `json:"result_summary,omitempty"`
	TranscriptPath  string     `json:"transcript_path,omitempty"`
	SkillsUsed      string     `json:"skills_used,omitempty"`
	FixApplied      string     `json:"fix_applied,omitempty"`
	SessionKey      string     `json:"session_key,omitempty"`
	ParentRunID     *int64     `json:"parent_run_id,omitempty"`
	RetryOf         *int64            `json:"retry_of,omitempty"`
	ToolCalls       []ToolCallRecord  `json:"tool_calls,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}
