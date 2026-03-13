package notification

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

func TestCreateAndGet(t *testing.T) {
	store := NewStore(testDB(t))
	n := &Notification{
		TaskName: "Daily digest",
		RunID:    1,
		Trigger:  "cron",
		Status:   "completed",
		Content:  "Here is your digest.",
	}
	id, err := store.Create(n)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	list, total, err := store.List("", 10, 0)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if list[0].TaskName != "Daily digest" {
		t.Errorf("expected 'Daily digest', got %q", list[0].TaskName)
	}
	if list[0].Read {
		t.Error("expected unread")
	}
}

func TestUnreadCount(t *testing.T) {
	store := NewStore(testDB(t))
	store.Create(&Notification{TaskName: "t1", Trigger: "cron", Status: "completed", Content: "a"})
	store.Create(&Notification{TaskName: "t2", Trigger: "manual", Status: "failed", Content: "b"})

	count, err := store.UnreadCount("")
	if err != nil {
		t.Fatalf("UnreadCount() error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 unread, got %d", count)
	}
}

func TestMarkRead(t *testing.T) {
	store := NewStore(testDB(t))
	id, _ := store.Create(&Notification{TaskName: "t1", Trigger: "cron", Status: "completed", Content: "a"})

	if err := store.MarkRead(id); err != nil {
		t.Fatalf("MarkRead() error: %v", err)
	}

	count, _ := store.UnreadCount("")
	if count != 0 {
		t.Errorf("expected 0 unread, got %d", count)
	}
}

func TestMarkAllRead(t *testing.T) {
	store := NewStore(testDB(t))
	store.Create(&Notification{TaskName: "t1", Trigger: "cron", Status: "completed", Content: "a"})
	store.Create(&Notification{TaskName: "t2", Trigger: "cron", Status: "completed", Content: "b"})

	if err := store.MarkAllRead(""); err != nil {
		t.Fatalf("MarkAllRead() error: %v", err)
	}

	count, _ := store.UnreadCount("")
	if count != 0 {
		t.Errorf("expected 0 unread, got %d", count)
	}
}

func TestUserScoping(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO users (id, email, password_hash, role) VALUES ('u1', 'alice', 'x', 'user')`)
	db.Exec(`INSERT INTO users (id, email, password_hash, role) VALUES ('u2', 'bob', 'x', 'user')`)

	store := NewStore(db)
	store.Create(&Notification{UserID: "u1", TaskName: "alice task", Trigger: "cron", Status: "completed", Content: "a"})
	store.Create(&Notification{UserID: "u2", TaskName: "bob task", Trigger: "cron", Status: "completed", Content: "b"})

	list, total, _ := store.List("u1", 10, 0)
	if total != 1 {
		t.Fatalf("expected 1 for u1, got %d", total)
	}
	if list[0].TaskName != "alice task" {
		t.Errorf("expected 'alice task', got %q", list[0].TaskName)
	}
}

func TestNullUserIDVisibleToAuthenticatedUsers(t *testing.T) {
	db := testDB(t)
	db.Exec(`INSERT INTO users (id, email, password_hash, role) VALUES ('u1', 'alice', 'x', 'user')`)

	store := NewStore(db)
	// Legacy notification with no user_id (from tasks created before multi-user).
	store.Create(&Notification{TaskName: "legacy task", Trigger: "cron", Status: "completed", Content: "legacy"})
	// Notification owned by u1.
	store.Create(&Notification{UserID: "u1", TaskName: "alice task", Trigger: "manual", Status: "completed", Content: "owned"})

	// Authenticated user u1 should see both.
	list, total, err := store.List("u1", 10, 0)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 for u1 (own + legacy), got %d", total)
	}
	names := map[string]bool{}
	for _, n := range list {
		names[n.TaskName] = true
	}
	if !names["legacy task"] || !names["alice task"] {
		t.Errorf("expected both legacy and owned tasks, got %v", names)
	}

	// Unread count should also include legacy notifications.
	count, err := store.UnreadCount("u1")
	if err != nil {
		t.Fatalf("UnreadCount() error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 unread, got %d", count)
	}

	// Unauthenticated user should only see legacy (NULL user_id).
	list2, total2, _ := store.List("", 10, 0)
	if total2 != 1 {
		t.Fatalf("expected 1 for unauthenticated, got %d", total2)
	}
	if list2[0].TaskName != "legacy task" {
		t.Errorf("expected 'legacy task', got %q", list2[0].TaskName)
	}
}

func TestCreateWithSenderID(t *testing.T) {
	store := NewStore(testDB(t))
	n := &Notification{
		UserID:   "user-1",
		SenderID: "user-2",
		TaskName: "Message from Alice",
		Trigger:  "user_message",
		Status:   "info",
		Content:  "The build is ready",
	}
	id, err := store.Create(n)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	list, _, err := store.List("user-1", 10, 0)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(list))
	}
	if list[0].SenderID != "user-2" {
		t.Errorf("expected sender_id 'user-2', got %q", list[0].SenderID)
	}
	if list[0].ID != id {
		t.Errorf("expected ID %d, got %d", id, list[0].ID)
	}
}

func TestPagination(t *testing.T) {
	store := NewStore(testDB(t))
	for i := 0; i < 5; i++ {
		store.Create(&Notification{TaskName: "t", Trigger: "cron", Status: "completed", Content: "x"})
	}

	list, total, _ := store.List("", 2, 0)
	if total != 5 {
		t.Fatalf("expected total 5, got %d", total)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 items, got %d", len(list))
	}

	list2, _, _ := store.List("", 2, 2)
	if len(list2) != 2 {
		t.Errorf("expected 2 items on page 2, got %d", len(list2))
	}
	if list2[0].ID == list[0].ID {
		t.Error("page 2 should return different items")
	}
}
