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

	// Unauthenticated request must be rejected with 401 (missing credentials),
	// not 403. Pinning the exact code catches a regression that swaps the
	// rejection path (e.g. returning 403 for a missing token).
	req := httptest.NewRequest("GET", "/api/admin/retrieval-traces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("unauthenticated status = %d, want exactly 401", w.Code)
	}

	// A non-admin (regular user) token authenticates fine but lacks the admin
	// role, so it must be rejected with 403 (forbidden), not 401.
	_, userTok := createTestUserWithToken(t, router, store, "alice@test.com", user.RoleUser)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, authReq("GET", "/api/admin/retrieval-traces", nil, userTok))
	if w.Code != 403 {
		t.Fatalf("non-admin status = %d, want exactly 403", w.Code)
	}
}

func TestRetrievalTracesAdminGets200(t *testing.T) {
	// Full-stack: an admin token must reach the data through the entire
	// middleware stack (auth + admin gate), not just the handler in isolation.
	router, store := setupUserRouter(t)
	ring := memory.NewTraceRing(8)
	ring.Record(&memory.RetrievalTrace{RequestID: "r1", UserID: "u1", SessionKey: "web:default"})
	ring.Record(&memory.RetrievalTrace{RequestID: "r2", UserID: "u2", SessionKey: "web:other"})
	router.retrievalTraces = ring

	_, adminTok := createTestUserWithToken(t, router, store, "admin@test.com", user.RoleAdmin)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("GET", "/api/admin/retrieval-traces", nil, adminTok))
	if w.Code != 200 {
		t.Fatalf("admin status = %d, want 200; body %s", w.Code, w.Body.String())
	}

	var body struct {
		Traces []memory.RetrievalTrace `json:"traces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(body.Traces) != 2 {
		t.Fatalf("traces = %+v, want 2 entries", body.Traces)
	}
	got := map[string]bool{}
	for _, tr := range body.Traces {
		got[tr.RequestID] = true
	}
	if !got["r1"] || !got["r2"] {
		t.Errorf("traces = %+v, want RequestIDs r1 and r2", body.Traces)
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
