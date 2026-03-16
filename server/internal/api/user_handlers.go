package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/user"
	"golang.org/x/crypto/bcrypt"
)

// handleListUsers returns all users. Admin only.
func (r *Router) handleListUsers(w http.ResponseWriter, req *http.Request) {
	users, err := r.users.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleGetUser returns a single user by ID. Admin only.
func (r *Router) handleGetUser(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	u, err := r.users.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// updateRoleRequest is the JSON body for PUT /api/users/{id}/role.
type updateRoleRequest struct {
	Role string `json:"role"`
}

// handleUpdateUserRole changes a user's role. Admin only.
// Cannot change own role. Cannot remove the last admin.
func (r *Router) handleUpdateUserRole(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetID := req.PathValue("id")

	var body updateRoleRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	newRole := user.Role(body.Role)
	if newRole != user.RoleAdmin && newRole != user.RoleModerator && newRole != user.RoleUser {
		writeError(w, http.StatusBadRequest, "invalid role")
		return
	}

	if targetID == caller.ID {
		writeError(w, http.StatusBadRequest, "cannot change own role")
		return
	}

	// Fetch the target user to check their current role.
	target, err := r.users.Get(targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// If demoting an admin, verify they are not the last one.
	if target.Role == user.RoleAdmin && newRole != user.RoleAdmin {
		adminCount, err := r.countAdmins()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count admins")
			return
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "cannot remove last admin")
			return
		}
	}

	if err := r.users.UpdateRole(targetID, newRole); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update role")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleResetPassword allows an admin to set a new password for any user.
func (r *Router) handleResetPassword(w http.ResponseWriter, req *http.Request) {
	targetID := req.PathValue("id")

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	existing, err := r.users.Get(targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	h := string(hash)
	if err := r.users.UpdateUser(targetID, existing.Name, &h); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteUser removes a user and all their private data. Admin only.
// Cannot delete self. Cannot delete the last admin.
func (r *Router) handleDeleteUser(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetID := req.PathValue("id")

	if targetID == caller.ID {
		writeError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}

	target, err := r.users.Get(targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Prevent deleting the last admin.
	if target.Role == user.RoleAdmin {
		adminCount, err := r.countAdmins()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to count admins")
			return
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "cannot delete last admin")
			return
		}
	}

	// Step 1: Disable and reassign tasks to the caller.
	if r.tasks != nil {
		if err := r.tasks.DisableAndReassignTasks(targetID, caller.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reassign tasks")
			return
		}
	}

	// Steps 2-4: Delete private data (nodes, edges, sessions, messages)
	// and orphaned invite codes that reference this user.
	if r.db != nil {
		cleanupQueries := []string{
			`DELETE FROM token_usage WHERE user_id = ?`,
			`DELETE FROM task_runs WHERE user_id = ?`,
			`DELETE FROM edges WHERE user_id = ?`,
			`DELETE FROM nodes WHERE user_id = ?`,
			`DELETE FROM messages WHERE user_id = ?`,
			`DELETE FROM sessions WHERE user_id = ?`,
			`UPDATE invite_codes SET redeemed_by = NULL WHERE redeemed_by = ?`,
			`DELETE FROM invite_codes WHERE created_by = ?`,
		}
		for _, q := range cleanupQueries {
			if _, err := r.db.Exec(q, targetID); err != nil {
				// Log but continue; best-effort cleanup.
			}
		}
	}

	// Step 5: Revoke all refresh tokens.
	if err := r.users.RevokeAllRefreshTokens(targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke tokens")
		return
	}

	// Step 6: Delete the user record.
	if err := r.users.Delete(targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// meResponse wraps User with computed fields for GET /api/me.
type meResponse struct {
	*user.User
	HasPassword bool `json:"has_password"`
}

// handleGetMe returns the authenticated user's own information.
func (r *Router) handleGetMe(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := r.users.Get(caller.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	hasPass, _ := r.users.HasPassword(caller.ID)
	writeJSON(w, http.StatusOK, meResponse{User: u, HasPassword: hasPass})
}

// updateMeRequest is the JSON body for PUT /api/me.
type updateMeRequest struct {
	CurrentPassword string `json:"current_password"`
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password,omitempty"`
}

// handleUpdateMe updates the caller's name, email, and/or password.
// Requires current_password for verification.
func (r *Router) handleUpdateMe(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body updateMeRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "current_password is required")
		return
	}

	current, err := r.users.Get(caller.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(current.PasswordHash), []byte(body.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "incorrect password")
		return
	}

	if body.Password != "" && len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	if body.Email != "" && body.Email != current.Email {
		if err := r.users.UpdateEmail(caller.ID, body.Email); err != nil {
			if errors.Is(err, user.ErrDuplicateUser) {
				writeError(w, http.StatusConflict, "email already taken")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to update email")
			return
		}
	}

	name := body.Name
	if name == "" {
		name = current.Name
	}

	var hashPtr *string
	if body.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		h := string(hash)
		hashPtr = &h
	}

	if err := r.users.UpdateUser(caller.ID, name, hashPtr); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}

	u, err := r.users.Get(caller.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch updated user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// handleGetProfile returns the caller's profile overrides.
func (r *Router) handleGetProfile(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := r.users.Get(caller.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"overrides": u.ProfileOverrides})
}

// updateProfileRequest is the JSON body for PUT /api/me/profile.
type updateProfileRequest struct {
	Overrides string `json:"overrides"`
}

// handleUpdateProfile sets the caller's profile overrides.
func (r *Router) handleUpdateProfile(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body updateProfileRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := r.users.UpdateProfile(caller.ID, body.Overrides); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"overrides": body.Overrides})
}

// countAdmins returns the number of users with the admin role.
func (r *Router) countAdmins() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, string(user.RoleAdmin)).Scan(&count)
	return count, err
}

