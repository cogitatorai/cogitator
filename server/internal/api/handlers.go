package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cogitatorai/cogitator/server/internal/auth"
)

func (r *Router) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// userFromRequest extracts the authenticated user from the request context.
// Returns a zero-value ContextUser when no auth context is present (e.g.
// in tests where JWTService is nil), allowing handlers to degrade gracefully.
func userFromRequest(r *http.Request) (auth.ContextUser, bool) {
	return auth.UserFromContext(r.Context())
}

// userIDFromRequest returns the authenticated user's ID, or empty string
// when no auth context is present.
func userIDFromRequest(r *http.Request) string {
	u, ok := userFromRequest(r)
	if !ok {
		return ""
	}
	return u.ID
}

// isAdmin returns true if the authenticated user has the admin role.
// Returns false when no auth context is present (e.g. tests).
func isAdmin(r *http.Request) bool {
	u, ok := userFromRequest(r)
	if !ok {
		return false
	}
	return u.Role == "admin"
}

// isAdminOrMod returns true if the authenticated user has the admin or moderator role.
func isAdminOrMod(r *http.Request) bool {
	u, ok := userFromRequest(r)
	if !ok {
		return false
	}
	return u.Role == "admin" || u.Role == "moderator"
}

// hasAuth returns true if an auth context is present in the request.
func hasAuth(r *http.Request) bool {
	_, ok := userFromRequest(r)
	return ok
}

// requireAdmin checks that the authenticated user has the admin role.
// Returns true if authorized; writes an error response and returns false otherwise.
// When no auth context is present (single-user mode), allows through.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	_, ok := userFromRequest(r)
	if !ok {
		// No auth context (single-user mode with JWTService nil): allow through.
		return true
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin access required")
		return false
	}
	return true
}

// canAccessNode returns true if the caller can see the given node.
// Public nodes (private=false) are visible to everyone.
// Private nodes are visible only to their owner or admin users.
func canAccessNode(r *http.Request, private bool, nodeUserID *string) bool {
	if !private {
		return true // public node
	}
	uid := userIDFromRequest(r)
	if uid == "" {
		return true // no auth context (single-user mode)
	}
	return nodeUserID != nil && *nodeUserID == uid || isAdmin(r)
}

// ownsResource checks that the caller owns a resource identified by its ownerID.
// Returns true if the caller is the owner, an admin, or no auth context exists.
// Writes 404 and returns false otherwise (404 instead of 403 to avoid leaking existence).
func ownsResource(w http.ResponseWriter, r *http.Request, ownerID string) bool {
	uid := userIDFromRequest(r)
	if uid == "" {
		return true // no auth context (single-user mode)
	}
	if ownerID == uid || isAdmin(r) {
		return true
	}
	writeError(w, http.StatusNotFound, "not found")
	return false
}
