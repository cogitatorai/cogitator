package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// createInviteCodeRequest is the JSON body for POST /api/invite-codes.
type createInviteCodeRequest struct {
	Role      string  `json:"role"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// handleCreateInviteCode generates a new invite code. Admin or moderator.
func (r *Router) handleCreateInviteCode(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body createInviteCodeRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	role := user.Role(body.Role)
	if role != user.RoleAdmin && role != user.RoleModerator && role != user.RoleUser {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}

	input := user.CreateInviteInput{
		CreatedBy: caller.ID,
		Role:      role,
	}

	if body.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at format; use RFC3339")
			return
		}
		input.ExpiresAt = &t
	}

	ic, err := r.users.CreateInviteCode(input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invite code")
		return
	}

	writeJSON(w, http.StatusCreated, ic)
}

// handleListInviteCodes returns all invite codes. Admin or moderator.
func (r *Router) handleListInviteCodes(w http.ResponseWriter, req *http.Request) {
	codes, err := r.users.ListInviteCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invite codes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"codes": codes})
}

// handleDeleteInviteCode revokes an invite code. Admin or moderator.
func (r *Router) handleDeleteInviteCode(w http.ResponseWriter, req *http.Request) {
	code := req.PathValue("code")
	if err := r.users.DeleteInviteCode(code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete invite code")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
