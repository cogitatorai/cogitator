package push

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

func TestUpsertAndList(t *testing.T) {
	store := NewStore(testDB(t))

	if err := store.Upsert("u1", "ExponentPushToken[abc]", "ios"); err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	tokens, err := store.ListByUser("u1")
	if err != nil {
		t.Fatalf("ListByUser() error: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Token != "ExponentPushToken[abc]" {
		t.Errorf("expected token 'ExponentPushToken[abc]', got %q", tokens[0].Token)
	}
}

func TestUpsertReassigns(t *testing.T) {
	store := NewStore(testDB(t))

	store.Upsert("u1", "ExponentPushToken[abc]", "ios")
	store.Upsert("u2", "ExponentPushToken[abc]", "ios") // same token, different user

	tokens, _ := store.ListByUser("u1")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for u1 after reassign, got %d", len(tokens))
	}

	tokens, _ = store.ListByUser("u2")
	if len(tokens) != 1 {
		t.Errorf("expected 1 token for u2, got %d", len(tokens))
	}
}

func TestDeleteByToken(t *testing.T) {
	store := NewStore(testDB(t))
	store.Upsert("u1", "ExponentPushToken[abc]", "ios")

	if err := store.DeleteByToken("ExponentPushToken[abc]"); err != nil {
		t.Fatalf("DeleteByToken() error: %v", err)
	}

	tokens, _ := store.ListByUser("u1")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens after delete, got %d", len(tokens))
	}
}

func TestDeleteByUser(t *testing.T) {
	store := NewStore(testDB(t))
	store.Upsert("u1", "ExponentPushToken[abc]", "ios")
	store.Upsert("u1", "ExponentPushToken[def]", "android")

	if err := store.DeleteByUser("u1"); err != nil {
		t.Fatalf("DeleteByUser() error: %v", err)
	}

	tokens, _ := store.ListByUser("u1")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens after user delete, got %d", len(tokens))
	}
}
