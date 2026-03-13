package task

import (
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/database"
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

// insertTestUser creates a minimal user row so that foreign key constraints
// on tasks.user_id are satisfied during testing.
func insertTestUser(t *testing.T, db *database.DB, id string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT OR IGNORE INTO users (id, email, password_hash, role) VALUES (?, ?, '', 'user')`,
		id, id,
	)
	if err != nil {
		t.Fatalf("insert test user %q: %v", id, err)
	}
}

func TestCreateAndGetTask(t *testing.T) {
	store := NewStore(testDB(t))

	tk := &Task{
		Name:         "daily summary",
		Prompt:       "Summarize today's events",
		CronExpr:     "0 22 * * *",
		Enabled:      true,
		MaxRetries:   3,
		RetryBackoff: 60,
		Timeout:      300,
		AllowManual:  true,
		CreatedBy:    "user",
	}

	id, err := store.CreateTask(tk)
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if got.Name != "daily summary" {
		t.Errorf("expected 'daily summary', got %q", got.Name)
	}
	if got.CronExpr != "0 22 * * *" {
		t.Errorf("expected cron expr, got %q", got.CronExpr)
	}
	if !got.Enabled {
		t.Error("expected enabled")
	}
	if got.ModelTier != "cheap" {
		t.Errorf("expected default model_tier 'cheap', got %q", got.ModelTier)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	store := NewStore(testDB(t))
	_, err := store.GetTask(999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateTask(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateTask(&Task{
		Name:   "original",
		Prompt: "original prompt",
	})

	tk, _ := store.GetTask(id)
	tk.Name = "updated"
	tk.Enabled = true

	err := store.UpdateTask(tk)
	if err != nil {
		t.Fatalf("UpdateTask() error: %v", err)
	}

	got, _ := store.GetTask(id)
	if got.Name != "updated" {
		t.Errorf("expected 'updated', got %q", got.Name)
	}
}

func TestDeleteTask(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateTask(&Task{Name: "delete me", Prompt: "p"})
	err := store.DeleteTask(id)
	if err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}

	_, err = store.GetTask(id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteTaskCascadesRuns(t *testing.T) {
	store := NewStore(testDB(t))

	id, _ := store.CreateTask(&Task{Name: "with runs", Prompt: "p"})
	store.CreateRun(&Run{TaskID: &id, Trigger: "manual"})
	store.CreateRun(&Run{TaskID: &id, Trigger: "cron"})

	runs, err := store.ListRunsForTask(id, 10)
	if err != nil {
		t.Fatalf("ListRunsForTask() error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs before delete, got %d", len(runs))
	}

	if err := store.DeleteTask(id); err != nil {
		t.Fatalf("DeleteTask() error: %v", err)
	}

	runs, err = store.ListRunsForTask(id, 10)
	if err != nil {
		t.Fatalf("ListRunsForTask() after delete error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs after task delete, got %d", len(runs))
	}
}

func TestListTasks(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{Name: "task A", Prompt: "a", Enabled: true})
	store.CreateTask(&Task{Name: "task B", Prompt: "b", Enabled: true})

	tasks, err := store.ListTasks("")
	if err != nil {
		t.Fatalf("ListTasks() error: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestListScheduledTasks(t *testing.T) {
	store := NewStore(testDB(t))

	store.CreateTask(&Task{Name: "scheduled", Prompt: "p", CronExpr: "0 * * * *", Enabled: true})
	store.CreateTask(&Task{Name: "no cron", Prompt: "p", Enabled: true})
	store.CreateTask(&Task{Name: "disabled cron", Prompt: "p", CronExpr: "0 * * * *", Enabled: false})

	tasks, err := store.ListScheduledTasks()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 scheduled task, got %d", len(tasks))
	}
	if tasks[0].Name != "scheduled" {
		t.Errorf("expected 'scheduled', got %q", tasks[0].Name)
	}
}

func TestCreateAndGetRun(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "test", Prompt: "p"})

	run := &Run{
		TaskID:  &taskID,
		Trigger: TriggerManual,
	}
	runID, err := store.CreateRun(run)
	if err != nil {
		t.Fatalf("CreateRun() error: %v", err)
	}

	got, err := store.GetRun(runID)
	if err != nil {
		t.Fatalf("GetRun() error: %v", err)
	}
	if got.Status != RunStatusRunning {
		t.Errorf("expected 'running', got %q", got.Status)
	}
	if got.Trigger != TriggerManual {
		t.Errorf("expected 'manual', got %q", got.Trigger)
	}
}

func TestCompleteRun(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "test", Prompt: "p"})
	runID, _ := store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerManual})

	err := store.CompleteRun(runID, "All done")
	if err != nil {
		t.Fatalf("CompleteRun() error: %v", err)
	}

	got, _ := store.GetRun(runID)
	if got.Status != RunStatusCompleted {
		t.Errorf("expected 'completed', got %q", got.Status)
	}
	if got.ResultSummary != "All done" {
		t.Errorf("expected 'All done', got %q", got.ResultSummary)
	}
	if got.FinishedAt == nil {
		t.Error("expected non-nil FinishedAt")
	}
}

func TestFailRun(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "test", Prompt: "p"})
	runID, _ := store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerSystem})

	err := store.FailRun(runID, "API timeout", ErrorTransient)
	if err != nil {
		t.Fatalf("FailRun() error: %v", err)
	}

	got, _ := store.GetRun(runID)
	if got.Status != RunStatusFailed {
		t.Errorf("expected 'failed', got %q", got.Status)
	}
	if got.ErrorMessage != "API timeout" {
		t.Errorf("expected 'API timeout', got %q", got.ErrorMessage)
	}
	if got.ErrorClass != string(ErrorTransient) {
		t.Errorf("expected 'transient', got %q", got.ErrorClass)
	}
}

func TestListRunsForTask(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "test", Prompt: "p"})
	store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerCron})
	store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerManual})

	runs, err := store.ListRunsForTask(taskID, 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

func TestRecentRuns(t *testing.T) {
	store := NewStore(testDB(t))

	taskID, _ := store.CreateTask(&Task{Name: "test", Prompt: "p"})
	store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerCron})
	store.CreateRun(&Run{TaskID: &taskID, Trigger: TriggerManual})

	runs, err := store.RecentRuns(5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
	// Most recent first
	if runs[0].ID < runs[1].ID {
		t.Error("expected descending order")
	}
}

func TestListTasks_FilteredByUser(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")

	store.CreateTask(&Task{Name: "alice task 1", Prompt: "p", Enabled: true, UserID: "alice"})
	store.CreateTask(&Task{Name: "alice task 2", Prompt: "p", Enabled: true, UserID: "alice"})
	store.CreateTask(&Task{Name: "bob task 1", Prompt: "p", Enabled: true, UserID: "bob"})

	// Filter by alice.
	tasks, err := store.ListTasks("alice")
	if err != nil {
		t.Fatalf("ListTasks(alice) error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks for alice, got %d", len(tasks))
	}
	for _, tk := range tasks {
		if tk.UserID != "alice" {
			t.Errorf("expected user_id alice, got %q", tk.UserID)
		}
	}

	// Filter by bob.
	tasks, err = store.ListTasks("bob")
	if err != nil {
		t.Fatalf("ListTasks(bob) error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task for bob, got %d", len(tasks))
	}
	if tasks[0].Name != "bob task 1" {
		t.Errorf("expected 'bob task 1', got %q", tasks[0].Name)
	}

	// Empty userID returns all tasks.
	tasks, err = store.ListTasks("")
	if err != nil {
		t.Fatalf("ListTasks('') error: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks for empty userID, got %d", len(tasks))
	}
}

func TestCreateTask_SetsUserID(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "user-42")

	id, err := store.CreateTask(&Task{
		Name:    "user-owned",
		Prompt:  "do something",
		Enabled: true,
		UserID:  "user-42",
	})
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}

	got, err := store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if got.UserID != "user-42" {
		t.Errorf("expected user_id 'user-42', got %q", got.UserID)
	}
}

func TestDisableAndReassignTasks(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "alice")
	insertTestUser(t, db, "bob")
	insertTestUser(t, db, "admin")

	id1, _ := store.CreateTask(&Task{Name: "a1", Prompt: "p", Enabled: true, UserID: "alice"})
	id2, _ := store.CreateTask(&Task{Name: "a2", Prompt: "p", Enabled: true, UserID: "alice"})
	id3, _ := store.CreateTask(&Task{Name: "b1", Prompt: "p", Enabled: true, UserID: "bob"})

	err := store.DisableAndReassignTasks("alice", "admin")
	if err != nil {
		t.Fatalf("DisableAndReassignTasks() error: %v", err)
	}

	// Alice's tasks are disabled and reassigned to admin.
	t1, _ := store.GetTask(id1)
	t2, _ := store.GetTask(id2)
	if t1.Enabled || t2.Enabled {
		t.Error("expected alice's tasks to be disabled")
	}
	if t1.UserID != "admin" || t2.UserID != "admin" {
		t.Errorf("expected user_id 'admin', got %q and %q", t1.UserID, t2.UserID)
	}

	// Bob's task is unchanged.
	t3, _ := store.GetTask(id3)
	if !t3.Enabled {
		t.Error("expected bob's task to remain enabled")
	}
	if t3.UserID != "bob" {
		t.Errorf("expected user_id 'bob', got %q", t3.UserID)
	}

	// Disabled tasks still appear in ListTasks (dashboard shows all tasks).
	tasks, _ := store.ListTasks("admin")
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for admin, got %d", len(tasks))
	}
}

func TestCreateTask_NotifyUsers(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	tsk := &Task{
		Name:        "backup-check",
		Prompt:      "Check backups",
		CronExpr:    "0 0 * * *",
		ModelTier:   "cheap",
		Enabled:     true,
		AllowManual: true,
		NotifyChat:  true,
		NotifyUsers: []string{"user-1", "user-2"},
	}
	id, err := store.CreateTask(tsk)
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}

	got, err := store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if len(got.NotifyUsers) != 2 {
		t.Fatalf("expected 2 notify_users, got %d", len(got.NotifyUsers))
	}
	if got.NotifyUsers[0] != "user-1" || got.NotifyUsers[1] != "user-2" {
		t.Errorf("unexpected notify_users: %v", got.NotifyUsers)
	}
}

func TestCreateTask_NotifyUsersWildcard(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	tsk := &Task{
		Name:        "broadcast-task",
		Prompt:      "Broadcast something",
		CronExpr:    "0 9 * * *",
		ModelTier:   "cheap",
		Enabled:     true,
		NotifyChat:  true,
		NotifyUsers: []string{"*"},
	}
	id, err := store.CreateTask(tsk)
	if err != nil {
		t.Fatalf("CreateTask() error: %v", err)
	}

	got, err := store.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask() error: %v", err)
	}
	if len(got.NotifyUsers) != 1 || got.NotifyUsers[0] != "*" {
		t.Errorf("expected [\"*\"], got %v", got.NotifyUsers)
	}
	// Broadcast flag should also be set for N-1 compat
	if !got.Broadcast {
		t.Error("expected Broadcast=true for wildcard notify_users")
	}
}

func TestListScheduledTasks_OnlyEnabled(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)
	insertTestUser(t, db, "u1")

	store.CreateTask(&Task{Name: "active cron", Prompt: "p", CronExpr: "0 * * * *", Enabled: true, UserID: "u1"})
	store.CreateTask(&Task{Name: "disabled cron", Prompt: "p", CronExpr: "0 * * * *", Enabled: false, UserID: "u1"})
	store.CreateTask(&Task{Name: "no cron", Prompt: "p", Enabled: true, UserID: "u1"})

	tasks, err := store.ListScheduledTasks()
	if err != nil {
		t.Fatalf("ListScheduledTasks() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 scheduled task, got %d", len(tasks))
	}
	if tasks[0].Name != "active cron" {
		t.Errorf("expected 'active cron', got %q", tasks[0].Name)
	}
	if tasks[0].UserID != "u1" {
		t.Errorf("expected user_id 'u1', got %q", tasks[0].UserID)
	}
}
