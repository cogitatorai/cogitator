package api

import (
	"encoding/json"
	"net/http"
)

type registerPushTokenRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

func (r *Router) handleRegisterPushToken(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var body registerPushTokenRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Token == "" || body.Platform == "" {
		writeError(w, http.StatusBadRequest, "token and platform are required")
		return
	}

	if err := r.pushTokens.Upsert(userID, body.Token, body.Platform); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register push token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleUnregisterPushTokens(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := r.pushTokens.DeleteByUser(userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unregister push tokens")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
