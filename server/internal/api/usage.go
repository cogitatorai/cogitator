package api

import (
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

func (r *Router) handleDailyTokenStats(w http.ResponseWriter, req *http.Request) {
	days := 14
	if v := req.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	if days > 90 {
		days = 90
	}

	// Admins see all usage; regular users see only their own.
	uid := userIDFromRequest(req)
	if isAdmin(req) {
		uid = ""
	}

	stats, err := r.db.DailyTokenStats(days, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats == nil {
		stats = []database.DailyStats{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats})
}
