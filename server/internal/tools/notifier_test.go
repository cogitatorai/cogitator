package tools

import (
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/notification"
)

func testNotifDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNotifierAdapter_NotifyUser(t *testing.T) {
	db := testNotifDB(t)
	notifStore := notification.NewStore(db)
	eventBus := bus.New()

	ch := eventBus.Subscribe(bus.UserNotification)
	defer eventBus.Unsubscribe(ch)

	adapter := &NotifierAdapter{
		Notifications: notifStore,
		EventBus:      eventBus,
	}

	err := adapter.NotifyUser("sender-1", "John", "recipient-1", "The build is ready")
	if err != nil {
		t.Fatalf("NotifyUser() error: %v", err)
	}

	// Check notification was created
	list, _, err := notifStore.List("recipient-1", 10, 0)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(list))
	}
	n := list[0]
	if n.UserID != "recipient-1" {
		t.Errorf("expected user_id 'recipient-1', got %q", n.UserID)
	}
	if n.SenderID != "sender-1" {
		t.Errorf("expected sender_id 'sender-1', got %q", n.SenderID)
	}
	if n.TaskName != "Message from John" {
		t.Errorf("expected task_name 'Message from John', got %q", n.TaskName)
	}
	if n.Trigger != "user_message" {
		t.Errorf("expected trigger 'user_message', got %q", n.Trigger)
	}
	if n.Status != "info" {
		t.Errorf("expected status 'info', got %q", n.Status)
	}
	if n.Content != "The build is ready" {
		t.Errorf("expected content 'The build is ready', got %q", n.Content)
	}

	// Check event was published
	select {
	case evt := <-ch:
		if evt.Type != bus.UserNotification {
			t.Errorf("expected UserNotification event, got %s", evt.Type)
		}
		if evt.Payload["recipient_id"] != "recipient-1" {
			t.Errorf("expected recipient_id 'recipient-1', got %v", evt.Payload["recipient_id"])
		}
		if evt.Payload["sender_name"] != "John" {
			t.Errorf("expected sender_name 'John', got %v", evt.Payload["sender_name"])
		}
	default:
		t.Error("expected UserNotification event to be published")
	}
}
