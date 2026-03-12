package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

func (r *Router) handleListSessions(w http.ResponseWriter, req *http.Request) {
	sessions, err := r.sessions.List(userIDFromRequest(req))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (r *Router) handleGetSession(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}

	userID := userIDFromRequest(req)
	sess, err := r.sessions.Get(key, userID)
	if err == sql.ErrNoRows && key == "tasks:output" && userID != "" {
		// Lazily create the pinned tasks:output session on first access.
		sess, err = r.sessions.GetOrCreate("tasks:output", "tasks", "tasks", userID, false)
	}
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get session")
		return
	}

	messages, err := r.sessions.GetMessages(key, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session":  sess,
		"messages": messages,
	})
}

func (r *Router) handleActivateSession(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}
	if err := r.sessions.SetActiveSession(key, userIDFromRequest(req)); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to activate session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "activated"})
}

func (r *Router) handleDeleteSession(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}

	if err := r.sessions.Delete(key, userIDFromRequest(req)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{
			Type: bus.SessionDeleted,
			Payload: map[string]any{
				"session_key": key,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (r *Router) handleClearMessages(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}
	// Verify the caller owns this session.
	if _, err := r.sessions.Get(key, userIDFromRequest(req)); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := r.sessions.TruncateMessages(key, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear messages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (r *Router) handleDeleteMessage(w http.ResponseWriter, req *http.Request) {
	key := req.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "session key is required")
		return
	}
	// Verify the caller owns this session.
	if _, err := r.sessions.Get(key, userIDFromRequest(req)); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	idStr := req.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	if err := r.sessions.DeleteMessage(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
