package session

import (
	"database/sql"
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

// createTestUser inserts a minimal user row so that foreign key constraints on
// user_id columns are satisfied in tests.
func createTestUser(t *testing.T, db *database.DB, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT OR IGNORE INTO users (id, email, password_hash) VALUES (?, ?, ?)`,
		id, id, "hash")
	if err != nil {
		t.Fatalf("create test user %q: %v", id, err)
	}
}

func TestGetOrCreate(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "u1")
	store := NewStore(db)

	sess, err := store.GetOrCreate("web:user1", "web", "user1", "u1", false)
	if err != nil {
		t.Fatalf("GetOrCreate() error: %v", err)
	}
	if sess.Key != "web:user1" {
		t.Errorf("expected key 'web:user1', got %q", sess.Key)
	}
	if sess.Channel != "web" {
		t.Errorf("expected channel 'web', got %q", sess.Channel)
	}

	// Second call should return same session
	sess2, err := store.GetOrCreate("web:user1", "web", "user1", "u1", false)
	if err != nil {
		t.Fatalf("second GetOrCreate() error: %v", err)
	}
	if sess2.Key != sess.Key {
		t.Errorf("expected same session key")
	}
}

func TestAddAndGetMessages(t *testing.T) {
	store := NewStore(testDB(t))
	store.GetOrCreate("test:1", "test", "1", "", false)

	store.AddMessage("test:1", Message{Role: "user", Content: "Hello"})
	store.AddMessage("test:1", Message{Role: "assistant", Content: "Hi there"})
	store.AddMessage("test:1", Message{Role: "user", Content: "How are you?"})

	messages, err := store.GetMessages("test:1", 0)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Errorf("expected first message 'Hello', got %q", messages[0].Content)
	}
	if messages[2].Content != "How are you?" {
		t.Errorf("expected last message 'How are you?', got %q", messages[2].Content)
	}
}

func TestMessageMetadataRoundTrip(t *testing.T) {
	store := NewStore(testDB(t))
	store.GetOrCreate("test:1", "test", "1", "", false)

	meta := `{"task_name":"my-task","status":"completed","trigger":"manual"}`
	store.AddMessage("test:1", Message{Role: "assistant", Content: "result", Metadata: meta})
	store.AddMessage("test:1", Message{Role: "assistant", Content: "no meta"})

	messages, err := store.GetMessages("test:1", 0)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Metadata != meta {
		t.Errorf("expected metadata %q, got %q", meta, messages[0].Metadata)
	}
	if messages[1].Metadata != "" {
		t.Errorf("expected empty metadata, got %q", messages[1].Metadata)
	}
}

func TestGetMessagesWithLimit(t *testing.T) {
	store := NewStore(testDB(t))
	store.GetOrCreate("test:1", "test", "1", "", false)

	for i := 0; i < 10; i++ {
		store.AddMessage("test:1", Message{Role: "user", Content: "msg"})
	}

	messages, err := store.GetMessages("test:1", 3)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
}

func TestMessageCount(t *testing.T) {
	store := NewStore(testDB(t))
	store.GetOrCreate("test:1", "test", "1", "", false)

	store.AddMessage("test:1", Message{Role: "user", Content: "a"})
	store.AddMessage("test:1", Message{Role: "assistant", Content: "b"})

	count, err := store.MessageCount("test:1")
	if err != nil {
		t.Fatalf("MessageCount() error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestSetSummary(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "u1")
	store := NewStore(db)
	store.GetOrCreate("test:1", "test", "1", "u1", false)

	store.SetSummary("test:1", "User discussed Go programming.")

	sess, _ := store.Get("test:1", "u1")
	if sess.Summary != "User discussed Go programming." {
		t.Errorf("expected summary, got %q", sess.Summary)
	}
}

func TestTruncateMessages(t *testing.T) {
	store := NewStore(testDB(t))
	store.GetOrCreate("test:1", "test", "1", "", false)

	for i := 0; i < 10; i++ {
		store.AddMessage("test:1", Message{Role: "user", Content: "msg"})
	}

	err := store.TruncateMessages("test:1", 3)
	if err != nil {
		t.Fatalf("TruncateMessages() error: %v", err)
	}

	count, _ := store.MessageCount("test:1")
	if count != 3 {
		t.Errorf("expected 3 messages after truncation, got %d", count)
	}
}

func TestDeleteSession(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "u1")
	store := NewStore(db)
	store.GetOrCreate("test:1", "test", "1", "u1", false)
	store.AddMessage("test:1", Message{Role: "user", Content: "hello"})

	err := store.Delete("test:1", "u1")
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err = store.Get("test:1", "u1")
	if err == nil {
		t.Error("expected error getting deleted session")
	}

	// Messages should be cascade deleted
	messages, _ := store.GetMessages("test:1", 0)
	if len(messages) != 0 {
		t.Errorf("expected 0 messages after session delete, got %d", len(messages))
	}
}

func TestListSessions(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "u1")
	store := NewStore(db)
	store.GetOrCreate("web:1", "web", "1", "u1", false)
	store.GetOrCreate("telegram:2", "telegram", "2", "u1", false)

	sessions, err := store.List("u1")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

// New tests for user scoping

func TestListSessions_FilteredByUser(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "userA")
	createTestUser(t, db, "userB")
	store := NewStore(db)
	store.GetOrCreate("web:a1", "web", "a1", "userA", false)
	store.GetOrCreate("web:a2", "web", "a2", "userA", false)
	store.GetOrCreate("web:b1", "web", "b1", "userB", false)

	sessA, err := store.List("userA")
	if err != nil {
		t.Fatalf("List(userA) error: %v", err)
	}
	if len(sessA) != 2 {
		t.Errorf("expected 2 sessions for userA, got %d", len(sessA))
	}

	sessB, err := store.List("userB")
	if err != nil {
		t.Fatalf("List(userB) error: %v", err)
	}
	if len(sessB) != 1 {
		t.Errorf("expected 1 session for userB, got %d", len(sessB))
	}

	// Non-existent user sees nothing.
	sessC, err := store.List("userC")
	if err != nil {
		t.Fatalf("List(userC) error: %v", err)
	}
	if len(sessC) != 0 {
		t.Errorf("expected 0 sessions for userC, got %d", len(sessC))
	}
}

func TestGetOrCreate_SetsUserID(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "user42")
	store := NewStore(db)

	sess, err := store.GetOrCreate("web:x", "web", "x", "user42", true)
	if err != nil {
		t.Fatalf("GetOrCreate() error: %v", err)
	}
	if sess.UserID != "user42" {
		t.Errorf("expected user_id 'user42', got %q", sess.UserID)
	}
	if !sess.Private {
		t.Error("expected private=true")
	}

	// Re-fetch from DB to confirm persistence.
	sess2, err := store.Get("web:x", "user42")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if sess2.UserID != "user42" {
		t.Errorf("persisted user_id: expected 'user42', got %q", sess2.UserID)
	}
	if !sess2.Private {
		t.Error("persisted private: expected true")
	}
}

func TestGetSession_OwnershipCheck(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "owner")
	createTestUser(t, db, "stranger")
	store := NewStore(db)
	store.GetOrCreate("web:owned", "web", "owned", "owner", false)

	// Owner can get it.
	sess, err := store.Get("web:owned", "owner")
	if err != nil {
		t.Fatalf("Get() by owner error: %v", err)
	}
	if sess.Key != "web:owned" {
		t.Errorf("expected key 'web:owned', got %q", sess.Key)
	}

	// Other user cannot.
	_, err = store.Get("web:owned", "stranger")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows for stranger, got %v", err)
	}
}

func TestDeleteSession_OwnershipCheck(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "owner")
	createTestUser(t, db, "stranger")
	store := NewStore(db)
	store.GetOrCreate("web:del", "web", "del", "owner", false)

	// Stranger tries to delete; the row should survive.
	err := store.Delete("web:del", "stranger")
	if err != nil {
		t.Fatalf("Delete() by stranger error: %v", err)
	}

	// Session still exists for the owner.
	sess, err := store.Get("web:del", "owner")
	if err != nil {
		t.Fatalf("session should still exist for owner: %v", err)
	}
	if sess.Key != "web:del" {
		t.Errorf("expected key 'web:del', got %q", sess.Key)
	}

	// Owner deletes successfully.
	err = store.Delete("web:del", "owner")
	if err != nil {
		t.Fatalf("Delete() by owner error: %v", err)
	}
	_, err = store.Get("web:del", "owner")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after owner delete, got %v", err)
	}
}

func TestCreateSession_PrivateFlag(t *testing.T) {
	db := testDB(t)
	createTestUser(t, db, "u1")
	store := NewStore(db)

	// Create a public session.
	pub, err := store.GetOrCreate("web:pub", "web", "pub", "u1", false)
	if err != nil {
		t.Fatalf("GetOrCreate(public) error: %v", err)
	}
	if pub.Private {
		t.Error("expected private=false for public session")
	}

	// Create a private session.
	priv, err := store.GetOrCreate("web:priv", "web", "priv", "u1", true)
	if err != nil {
		t.Fatalf("GetOrCreate(private) error: %v", err)
	}
	if !priv.Private {
		t.Error("expected private=true for private session")
	}

	// Verify via List.
	sessions, err := store.List("u1")
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	privCount := 0
	for _, s := range sessions {
		if s.Private {
			privCount++
		}
	}
	if privCount != 1 {
		t.Errorf("expected 1 private session in list, got %d", privCount)
	}
}
