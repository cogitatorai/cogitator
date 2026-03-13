package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

func TestMarkTaskNotificationsRead(t *testing.T) {
	router := setupTestRouter(t)

	// Create a real task so the FK constraint on notifications.task_id is satisfied.
	taskID, err := router.tasks.CreateTask(&task.Task{
		Name: "hello-world", Prompt: "say hi", ModelTier: "smart",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := router.notifications.Create(&notification.Notification{
		TaskID: &taskID, TaskName: "hello-world", RunID: 1, Trigger: "manual", Status: "completed", Content: "done",
	}); err != nil {
		t.Fatalf("create notification: %v", err)
	}

	// Verify it's unread.
	req := httptest.NewRequest("GET", "/api/notifications", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp struct {
		Unread int `json:"unread"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Unread != 1 {
		t.Fatalf("expected unread 1, got %d", resp.Unread)
	}

	// PUT /api/notifications/read-tasks should mark it as read.
	req = httptest.NewRequest("PUT", "/api/notifications/read-tasks", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("read-tasks: got %d, want 200", w.Code)
	}

	// Verify unread is now 0.
	req = httptest.NewRequest("GET", "/api/notifications", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Unread != 0 {
		t.Errorf("expected unread 0 after read-tasks, got %d", resp.Unread)
	}
}

func TestNotificationEndpoints(t *testing.T) {
	router := setupTestRouter(t)

	// Create some test notifications directly in the store.
	router.notifications.Create(&notification.Notification{
		TaskName: "Task A", RunID: 1, Trigger: "cron", Status: "completed", Content: "Result A",
	})
	router.notifications.Create(&notification.Notification{
		TaskName: "Task B", RunID: 2, Trigger: "manual", Status: "failed", Content: "Error B",
	})

	// GET /api/notifications
	req := httptest.NewRequest("GET", "/api/notifications", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", w.Code)
	}

	var resp struct {
		Notifications []notification.Notification `json:"notifications"`
		Total         int                         `json:"total"`
		Unread        int                         `json:"unread"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if resp.Unread != 2 {
		t.Errorf("expected unread 2, got %d", resp.Unread)
	}

	// PUT /api/notifications/{id}/read
	id := resp.Notifications[0].ID
	req = httptest.NewRequest("PUT", fmt.Sprintf("/api/notifications/%d/read", id), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mark read: got %d, want 200", w.Code)
	}

	// Verify unread count decreased.
	req = httptest.NewRequest("GET", "/api/notifications", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Unread != 1 {
		t.Errorf("expected unread 1, got %d", resp.Unread)
	}

	// PUT /api/notifications/read-all
	req = httptest.NewRequest("PUT", "/api/notifications/read-all", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("read-all: got %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/notifications", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Unread != 0 {
		t.Errorf("expected unread 0, got %d", resp.Unread)
	}
}
