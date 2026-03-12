package api

import (
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

// GET /api/audit/logs?limit=50&offset=0&action=tool_blocked&outcome=blocked
func (r *Router) handleListAuditLogs(w http.ResponseWriter, req *http.Request) {
	q := database.AuditQuery{Limit: 50}

	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			q.Limit = parsed
		}
	}
	if o := req.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			q.Offset = parsed
		}
	}
	q.Action = req.URL.Query().Get("action")
	q.Outcome = req.URL.Query().Get("outcome")

	// Admins see all audit logs; regular users see only their own.
	uid := userIDFromRequest(req)
	if !isAdmin(req) {
		q.UserID = uid
	}

	entries, total, err := r.db.ListAuditLogs(q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   total,
	})
}
