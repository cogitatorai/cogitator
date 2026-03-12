package push

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

func TestSenderSend(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db)
	store.Upsert("u1", "ExponentPushToken[abc]", "ios")
	store.Upsert("u1", "ExponentPushToken[def]", "android")

	var received []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		tickets := make([]expoTicket, len(received))
		for i := range tickets {
			tickets[i] = expoTicket{Status: "ok", ID: "ticket-" + received[i].To}
		}
		json.NewEncoder(w).Encode(expoResponse{Data: tickets})
	}))
	defer srv.Close()

	sender := NewSender(store, nil)
	sender.apiURL = srv.URL

	count := sender.Send("u1", "Test", "Hello", 5, map[string]any{"type": "task"})
	if count != 2 {
		t.Errorf("expected 2 sends, got %d", count)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}
	// iOS should have badge, Android should not.
	if received[0].Badge == nil {
		t.Error("expected badge on iOS token")
	}
	if received[1].Badge != nil {
		t.Error("expected no badge on Android token")
	}
}

func TestSenderRemovesInvalidTokens(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := NewStore(db)
	store.Upsert("u1", "ExponentPushToken[bad]", "ios")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(expoResponse{
			Data: []expoTicket{{
				Status:  "error",
				Details: &expoDetails{Error: "DeviceNotRegistered"},
			}},
		})
	}))
	defer srv.Close()

	sender := NewSender(store, nil)
	sender.apiURL = srv.URL

	sender.Send("u1", "Test", "Hello", 0, nil)

	tokens, _ := store.ListByUser("u1")
	if len(tokens) != 0 {
		t.Errorf("expected invalid token to be removed, got %d tokens", len(tokens))
	}
}

func TestSenderNoTokens(t *testing.T) {
	store := NewStore(testDB(t))
	sender := NewSender(store, nil)
	count := sender.Send("nobody", "Test", "Hello", 0, nil)
	if count != 0 {
		t.Errorf("expected 0 sends for user with no tokens, got %d", count)
	}
}
