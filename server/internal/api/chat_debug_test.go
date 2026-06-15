package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// setupChatDebugRouter wires a router with an agent whose retriever uses a mock
// embedder (so the vector path runs) plus Users + JWTService for admin auth.
func setupChatDebugRouter(t *testing.T) (*Router, *user.Store) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	mock := provider.NewMock(provider.Response{Content: "hi"})
	mock.EmbedResponse = [][]float32{{1, 0, 0}}

	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	ret := memory.NewRetriever(memory.RetrieverConfig{Store: memStore, Embedder: mock})

	a := agent.New(agent.Config{
		Provider:       mock,
		Sessions:       sessStore,
		ContextBuilder: agent.NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "test",
		Retriever:      &agent.RetrieverAdapter{Retriever: ret},
	})

	users := user.NewStore(db)
	jwtSvc := auth.NewJWTService("test-secret-key-for-chat-debug", 15*time.Minute, 7*24*time.Hour)

	router := NewRouter(RouterConfig{
		DB:         db,
		Agent:      a,
		Sessions:   sessStore,
		Memory:     memStore,
		Users:      users,
		JWTService: jwtSvc,
	})

	return router, users
}

func decodeChatTrace(t *testing.T, body []byte) *memory.RetrievalTrace {
	t.Helper()
	var resp struct {
		Content        string                 `json:"content"`
		SessionKey     string                 `json:"session_key"`
		RetrievalTrace *memory.RetrievalTrace `json:"retrieval_trace"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode chat response: %v (body=%s)", err, string(body))
	}
	return resp.RetrievalTrace
}

func TestChatDebugRetrieval_AdminGetsTrace(t *testing.T) {
	router, store := setupChatDebugRouter(t)
	_, adminTok := createTestUserWithToken(t, router, store, "admin@test.com", user.RoleAdmin)

	payload := []byte(`{"message":"hello","channel":"web","chat_id":"c1"}`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/chat?debug=retrieval", payload, adminTok))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	trace := decodeChatTrace(t, w.Body.Bytes())
	if trace == nil {
		t.Fatalf("expected non-nil retrieval_trace, got nil; body=%s", w.Body.String())
	}
	if trace.Path != "vector" {
		t.Errorf("expected trace path 'vector', got %q", trace.Path)
	}
}

func TestChatDebugRetrieval_NoDebugNoTrace(t *testing.T) {
	router, store := setupChatDebugRouter(t)
	_, adminTok := createTestUserWithToken(t, router, store, "admin@test.com", user.RoleAdmin)

	payload := []byte(`{"message":"hello","channel":"web","chat_id":"c1"}`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/chat", payload, adminTok))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if trace := decodeChatTrace(t, w.Body.Bytes()); trace != nil {
		t.Errorf("expected no retrieval_trace without ?debug, got %+v", trace)
	}
}

func TestChatDebugRetrieval_NonAdminNoTrace(t *testing.T) {
	router, store := setupChatDebugRouter(t)
	_, userTok := createTestUserWithToken(t, router, store, "alice@test.com", user.RoleUser)

	payload := []byte(`{"message":"hello","channel":"web","chat_id":"c1"}`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/chat?debug=retrieval", payload, userTok))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if trace := decodeChatTrace(t, w.Body.Bytes()); trace != nil {
		t.Errorf("expected no retrieval_trace for non-admin, got %+v", trace)
	}
}
