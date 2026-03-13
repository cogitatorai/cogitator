package tools

import (
	"fmt"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/notification"
)

// NotifierAdapter implements UserNotifier by persisting notifications
// and publishing bus events for push delivery.
type NotifierAdapter struct {
	Notifications *notification.Store
	EventBus      *bus.Bus
}

func (a *NotifierAdapter) NotifyUser(senderID, senderName, recipientID, message string) error {
	taskName := "Message from " + senderName
	if senderName == "" {
		taskName = "Message"
	}

	_, err := a.Notifications.Create(&notification.Notification{
		UserID:   recipientID,
		SenderID: senderID,
		TaskName: taskName,
		Trigger:  "user_message",
		Status:   "info",
		Content:  message,
	})
	if err != nil {
		return fmt.Errorf("creating notification: %w", err)
	}

	if a.EventBus != nil {
		a.EventBus.Publish(bus.Event{
			Type: bus.UserNotification,
			Payload: map[string]any{
				"recipient_id": recipientID,
				"sender_id":    senderID,
				"sender_name":  senderName,
				"content":      message,
			},
		})
	}

	return nil
}
