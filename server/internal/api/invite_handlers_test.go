package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/user"
)

func TestCreateInviteCode_AdminOrMod(t *testing.T) {
	router, store := setupUserRouter(t)

	admin, adminTok := createTestUserWithToken(t, router, store, "admin", user.RoleAdmin)
	mod, modTok := createTestUserWithToken(t, router, store, "mod", user.RoleModerator)
	_, userTok := createTestUserWithToken(t, router, store, "alice", user.RoleUser)
	_ = admin
	_ = mod

	body, _ := json.Marshal(createInviteCodeRequest{Role: "user"})

	// Admin: 201.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/invite-codes", body, adminTok))
	if w.Code != http.StatusCreated {
		t.Fatalf("admin: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ic user.InviteCode
	json.NewDecoder(w.Body).Decode(&ic)
	if ic.Code == "" {
		t.Error("expected non-empty invite code")
	}
	if ic.Role != user.RoleUser {
		t.Errorf("expected role user, got %q", ic.Role)
	}

	// Moderator: 201.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/invite-codes", body, modTok))
	if w.Code != http.StatusCreated {
		t.Fatalf("moderator: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Regular user: 403.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, authReq("POST", "/api/invite-codes", body, userTok))
	if w.Code != http.StatusForbidden {
		t.Errorf("user: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListInviteCodes(t *testing.T) {
	router, store := setupUserRouter(t)

	admin, adminTok := createTestUserWithToken(t, router, store, "admin", user.RoleAdmin)

	// Create a couple of invite codes.
	_, _ = store.CreateInviteCode(user.CreateInviteInput{CreatedBy: admin.ID, Role: user.RoleUser})
	_, _ = store.CreateInviteCode(user.CreateInviteInput{CreatedBy: admin.ID, Role: user.RoleModerator})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("GET", "/api/invite-codes", nil, adminTok))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Codes []user.InviteCode `json:"codes"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Codes) != 2 {
		t.Errorf("expected 2 invite codes, got %d", len(resp.Codes))
	}
}

func TestDeleteInviteCode(t *testing.T) {
	router, store := setupUserRouter(t)

	admin, adminTok := createTestUserWithToken(t, router, store, "admin", user.RoleAdmin)

	ic, err := store.CreateInviteCode(user.CreateInviteInput{CreatedBy: admin.ID, Role: user.RoleUser})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, authReq("DELETE", "/api/invite-codes/"+ic.Code, nil, adminTok))
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify code is gone.
	codes, _ := store.ListInviteCodes()
	if len(codes) != 0 {
		t.Errorf("expected 0 invite codes after deletion, got %d", len(codes))
	}
}
