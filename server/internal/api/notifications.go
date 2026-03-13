package api

import (
	"net/http"
	"strconv"

	"github.com/cogitatorai/cogitator/server/internal/bus"
)

func (r *Router) handleListNotifications(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)

	limit := 50
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if v := req.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	list, total, err := r.notifications.List(userID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}

	unread, err := r.notifications.UnreadCount(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count unread")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": list,
		"total":         total,
		"unread":        unread,
	})
}

func (r *Router) handleMarkNotificationRead(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification ID")
		return
	}

	if err := r.notifications.MarkRead(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark read")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.NotificationsRead})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleMarkAllNotificationsRead(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)

	if err := r.notifications.MarkAllRead(userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark all read")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.NotificationsRead})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleMarkTaskNotificationsRead(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)

	if err := r.notifications.MarkTaskNotificationsRead(userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark task notifications read")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.NotificationsRead})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleDeleteNotification(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification ID")
		return
	}

	if err := r.notifications.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.NotificationsRead})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) handleDeleteAllNotifications(w http.ResponseWriter, req *http.Request) {
	userID := userIDFromRequest(req)

	if err := r.notifications.DeleteAll(userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notifications")
		return
	}

	if r.eventBus != nil {
		r.eventBus.Publish(bus.Event{Type: bus.NotificationsRead})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
