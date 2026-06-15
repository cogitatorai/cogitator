package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

func TestRetrievalTracesRequiresAdmin(t *testing.T) {
	// setupUserRouter wires Users + JWTService so the admin-gated routes are
	// actually registered (the route block in router.go is guarded by those).
	router, store := setupUserRouter(t)
	ring := memory.NewTraceRing(8)
	ring.Record(&memory.RetrievalTrace{RequestID: "r1", UserID: "u1", SessionKey: "web:default"})
	router.retrievalTraces = ring

	// Unauthenticated request should be rejected by the route's admin gate.
	req := httptest.NewRequest("GET", "/api/admin/retrieval-traces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 && w.Code != 401 {
		t.Fatalf("unauthenticated status = %d, want 401/403", w.Code)
	}

	// A non-admin (regular user) token should also be rejected.
	_, userTok := createTestUserWithToken(t, router, store, "alice@test.com", user.RoleUser)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, authReq("GET", "/api/admin/retrieval-traces", nil, userTok))
	if w.Code != 403 && w.Code != 401 {
		t.Fatalf("non-admin status = %d, want 401/403", w.Code)
	}
}

func TestRetrievalTracesHandlerReturnsRing(t *testing.T) {
	router := setupTestRouter(t)
	ring := memory.NewTraceRing(8)
	ring.Record(&memory.RetrievalTrace{RequestID: "r1", UserID: "u1", SessionKey: "web:default"})
	ring.Record(&memory.RetrievalTrace{RequestID: "r2", UserID: "u2", SessionKey: "web:other"})
	router.retrievalTraces = ring

	// Call the handler directly to bypass auth middleware and assert payload + filtering.
	req := httptest.NewRequest("GET", "/api/admin/retrieval-traces?session=web:default", nil)
	w := httptest.NewRecorder()
	router.handleRetrievalTraces(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d; body %s", w.Code, w.Body.String())
	}
	var body struct {
		Traces []memory.RetrievalTrace `json:"traces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Traces) != 1 || body.Traces[0].RequestID != "r1" {
		t.Errorf("filtered traces = %+v, want only r1", body.Traces)
	}
}
