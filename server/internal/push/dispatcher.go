package push

import (
	"log/slog"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/session"
)

// Dispatcher listens to bus events and sends push notifications.
type Dispatcher struct {
	sender        *Sender
	eventBus      *bus.Bus
	notifications *notification.Store
	sessions      *session.Store
	logger        *slog.Logger
	stop          chan struct{}
}

func NewDispatcher(
	sender *Sender,
	eventBus *bus.Bus,
	notifications *notification.Store,
	sessions *session.Store,
	logger *slog.Logger,
) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		sender:        sender,
		eventBus:      eventBus,
		notifications: notifications,
		sessions:      sessions,
		logger:        logger,
	}
}

func (d *Dispatcher) Start() {
	d.stop = make(chan struct{})
	ch := d.eventBus.Subscribe(bus.TaskNotifyChat, bus.MessageResponded, bus.UserNotification)

	go func() {
		for {
			select {
			case <-d.stop:
				d.eventBus.Unsubscribe(ch)
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				switch evt.Type {
				case bus.TaskNotifyChat:
					d.handleTaskNotification(evt)
				case bus.MessageResponded:
					d.handleChatResponse(evt)
				case bus.UserNotification:
					d.handleUserNotification(evt)
				}
			}
		}
	}()
}

func (d *Dispatcher) Stop() {
	if d.stop != nil {
		close(d.stop)
	}
}

func (d *Dispatcher) handleTaskNotification(evt bus.Event) {
	userID, _ := evt.Payload["user_id"].(string)
	if userID == "" {
		return
	}

	result, _ := evt.Payload["result"].(string)

	status := "completed"
	if strings.HasPrefix(result, "Failed:") {
		status = "failed"
	}

	title := "Task completed"
	body := "Your task finished successfully"
	if status == "failed" {
		title = "Task failed"
		body = "Your task encountered an error"
	}

	data := map[string]any{"page": "notifications"}

	// Prefer notify_users over broadcast flag.
	if notifyUsers, ok := evt.Payload["notify_users"].([]string); ok && len(notifyUsers) > 0 {
		if len(notifyUsers) == 1 && notifyUsers[0] == "*" {
			d.sender.SendToAll(title, body, "task", data)
		} else {
			for _, uid := range notifyUsers {
				badge := 0
				if d.notifications != nil {
					badge, _ = d.notifications.UnreadCount(uid)
				}
				d.sender.SendToUser(uid, title, body, "task", badge, data)
			}
		}
		return
	}

	// N-1 fallback: use broadcast flag.
	broadcast, _ := evt.Payload["broadcast"].(bool)
	if broadcast {
		d.sender.SendToAll(title, body, "task", data)
	} else {
		badge := 0
		if d.notifications != nil {
			badge, _ = d.notifications.UnreadCount(userID)
		}
		d.sender.SendToUser(userID, title, body, "task", badge, data)
	}
}

func (d *Dispatcher) handleUserNotification(evt bus.Event) {
	recipientID, _ := evt.Payload["recipient_id"].(string)
	senderName, _ := evt.Payload["sender_name"].(string)
	if recipientID == "" {
		return
	}

	title := "Message from " + senderName
	if senderName == "" {
		title = "New message"
	}
	body, _ := evt.Payload["content"].(string)
	if len(body) > 100 {
		body = body[:97] + "..."
	}

	badge := 0
	if d.notifications != nil {
		badge, _ = d.notifications.UnreadCount(recipientID)
	}

	d.sender.SendToUser(recipientID, title, body, "user_message", badge, map[string]any{
		"page": "notifications",
	})
}

func (d *Dispatcher) handleChatResponse(evt bus.Event) {
	sessionKey, _ := evt.Payload["session_key"].(string)
	ch, _ := evt.Payload["channel"].(string)

	if ch != "web" || sessionKey == "" {
		return
	}

	if d.sessions == nil {
		return
	}
	sess, err := d.sessions.Get(sessionKey, "")
	if err != nil || sess.UserID == "" {
		return
	}

	title := "New response"
	body := "Your agent responded"

	badge := 0
	if d.notifications != nil {
		badge, _ = d.notifications.UnreadCount(sess.UserID)
	}

	d.sender.SendToUser(sess.UserID, title, body, "chat", badge, map[string]any{
		"session_key": sessionKey,
	})
}
