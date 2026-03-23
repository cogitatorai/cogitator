package channel

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"nhooyr.io/websocket"
)

type wsMessage struct {
	Type          string          `json:"type"`
	Message       string          `json:"message,omitempty"`
	SessionKey    string          `json:"session_key,omitempty"`
	ChatID        string          `json:"chat_id,omitempty"`
	Content       string          `json:"content,omitempty"`
	Error         string          `json:"error,omitempty"`
	Status        string          `json:"status,omitempty"`
	Tool          string          `json:"tool,omitempty"`
	ToolsUsed     json.RawMessage `json:"tools_used,omitempty"`
	Private       bool            `json:"private,omitempty"`
	VoiceData     string          `json:"voice_data,omitempty"`
	VoiceFormat   string          `json:"voice_format,omitempty"`
	VoiceDuration int             `json:"voice_duration,omitempty"`
	MessageID     string          `json:"message_id,omitempty"`
}

// connInfo tracks per-connection ownership: which session it is bound to and
// which authenticated user owns it.
type connInfo struct {
	sessionKey string
	userID     string
}

// WebChannel handles WebSocket connections for real-time chat.
type WebChannel struct {
	handler       MessageHandler
	eventBus      *bus.Bus
	sessions      *session.Store
	notifications *notification.Store
	taskNameFunc  func(int64) string   // resolves task ID to name
	userIDsFunc   func() []string      // returns all user IDs (for broadcast notifications)
	logger        *slog.Logger
	mu            sync.Mutex
	conns         map[*websocket.Conn]connInfo
	activeReqs    map[string]context.CancelFunc // keyed by session key
	stopNotify    chan struct{}
}

func NewWebChannel(handler MessageHandler, eventBus *bus.Bus, sessions *session.Store, notifications *notification.Store, taskNameFunc func(int64) string, logger *slog.Logger) *WebChannel {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebChannel{
		handler:       handler,
		eventBus:      eventBus,
		sessions:      sessions,
		notifications: notifications,
		taskNameFunc:  taskNameFunc,
		logger:        logger,
		conns:         make(map[*websocket.Conn]connInfo),
		activeReqs:    make(map[string]context.CancelFunc),
	}
}

// SetUserIDsFunc sets the function used to resolve all user IDs for broadcast
// notifications. When nil, broadcast notifications fall back to the task owner.
func (wc *WebChannel) SetUserIDsFunc(fn func() []string) {
	wc.userIDsFunc = fn
}

func (wc *WebChannel) Name() string { return "web" }

func (wc *WebChannel) Start(ctx context.Context) error {
	if wc.eventBus != nil {
		wc.startVoiceSubscriber(ctx)
		wc.stopNotify = make(chan struct{})
		ch := wc.eventBus.Subscribe(bus.TaskNotifyChat, bus.TaskCompleted, bus.TaskFailed, bus.UserNotification)
		go func() {
			for {
				select {
				case <-wc.stopNotify:
					wc.eventBus.Unsubscribe(ch)
					return
				case evt, ok := <-ch:
					if !ok {
						return
					}
					switch evt.Type {
					case bus.TaskNotifyChat:
						result, _ := evt.Payload["result"].(string)
						taskID, _ := evt.Payload["task_id"].(int64)
						taskName, _ := evt.Payload["task_name"].(string)
						runID, _ := evt.Payload["run_id"].(int64)
						userID, _ := evt.Payload["user_id"].(string)
						trigger, _ := evt.Payload["trigger"].(string)
						status := "completed"
						if strings.HasPrefix(result, "Failed:") {
							status = "failed"
						}
						// Resolve recipients from notify_users (preferred) or broadcast (N-1 fallback)
						var recipients []string
						if notifyUsers, ok := evt.Payload["notify_users"].([]string); ok && len(notifyUsers) > 0 {
							if len(notifyUsers) == 1 && notifyUsers[0] == "*" {
								if wc.userIDsFunc != nil {
									recipients = wc.userIDsFunc()
								} else {
									recipients = []string{userID}
								}
							} else {
								recipients = notifyUsers
							}
						} else {
							broadcastFlag, _ := evt.Payload["broadcast"].(bool)
							if broadcastFlag && wc.userIDsFunc != nil {
								recipients = wc.userIDsFunc()
							} else {
								recipients = []string{userID}
							}
						}
						if wc.notifications != nil {
							tid := taskID
							created := 0
							for _, uid := range recipients {
								if _, err := wc.notifications.Create(&notification.Notification{
									UserID:   uid,
									TaskID:   &tid,
									TaskName: taskName,
									RunID:    runID,
									Trigger:  trigger,
									Status:   status,
									Content:  result,
								}); err != nil {
									wc.logger.Error("failed to create notification", "task", taskName, "user", uid, "error", err)
								} else {
									created++
								}
							}
							// Fallback to task owner if all recipient IDs were invalid
							if created == 0 && userID != "" {
								wc.notifications.Create(&notification.Notification{
									UserID:   userID,
									TaskID:   &tid,
									TaskName: taskName,
									RunID:    runID,
									Trigger:  trigger,
									Status:   status,
									Content:  result,
								})
							}
						}
						// Write task output to each recipient's per-user Tasks session.
						if wc.sessions != nil {
							triggerLabel := trigger
							if triggerLabel == "cron" {
								triggerLabel = "automated"
							}
							meta, _ := json.Marshal(map[string]any{
								"task_name": taskName,
								"task_id":   taskID,
								"run_id":    runID,
								"trigger":   triggerLabel,
								"status":    status,
							})
							writeTargets := recipients
							if len(writeTargets) == 0 {
								writeTargets = []string{userID}
							}
							for _, uid := range writeTargets {
								sk := session.TasksOutputKey(uid)
								if _, err := wc.sessions.GetOrCreate(sk, "tasks", "tasks", uid, false); err != nil {
									wc.logger.Error("failed to create tasks:output session", "user", uid, "error", err)
								} else if _, err := wc.sessions.AddMessage(sk, session.Message{
									SessionKey: sk,
									UserID:     uid,
									Role:       "assistant",
									Content:    result,
									Metadata:   string(meta),
								}); err != nil {
									wc.logger.Error("failed to write task output message", "user", uid, "error", err)
								}
							}
							// Notify all connected clients to refresh their Tasks view.
							wc.broadcast(wsMessage{
								Type:       "session_update",
								SessionKey: "tasks:output",
							})
						}
						notifMsg := wsMessage{
							Type:    "notification",
							Content: taskName,
							Status:  status,
						}
						if len(recipients) > 0 {
							wc.sendToUsers(recipients, notifMsg)
						} else {
							wc.sendToUser(userID, notifMsg)
						}
					case bus.TaskCompleted:
						taskID, _ := evt.Payload["task_id"].(int64)
						taskName := ""
						if wc.taskNameFunc != nil {
							taskName = wc.taskNameFunc(taskID)
						}
						wc.broadcast(wsMessage{
							Type:    "task_completed",
							Content: taskName,
							Status:  "completed",
						})
					case bus.TaskFailed:
						taskID, _ := evt.Payload["task_id"].(int64)
						taskName := ""
						if wc.taskNameFunc != nil {
							taskName = wc.taskNameFunc(taskID)
						}
						errMsg, _ := evt.Payload["error"].(string)
						wc.broadcast(wsMessage{
							Type:    "task_failed",
							Content: taskName,
							Status:  "failed",
							Error:   errMsg,
						})
					case bus.UserNotification:
						recipientID, _ := evt.Payload["recipient_id"].(string)
						senderName, _ := evt.Payload["sender_name"].(string)
						content, _ := evt.Payload["content"].(string)
						if recipientID != "" {
							// Write to the recipient's per-user Tasks session.
							if wc.sessions != nil {
								sk := session.TasksOutputKey(recipientID)
								msgContent := "Message from " + senderName + "\n\n" + content
								if _, err := wc.sessions.GetOrCreate(sk, "tasks", "tasks", recipientID, false); err != nil {
									wc.logger.Error("failed to create tasks:output session for notification", "error", err)
								} else if _, err := wc.sessions.AddMessage(sk, session.Message{
									SessionKey: sk,
									UserID:     recipientID,
									Role:       "assistant",
									Content:    msgContent,
								}); err != nil {
									wc.logger.Error("failed to write user notification message", "error", err)
								} else {
									wc.broadcast(wsMessage{
										Type:       "session_update",
										SessionKey: "tasks:output",
									})
								}
							}
							wc.sendToUser(recipientID, wsMessage{
								Type:    "notification",
								Content: "Message from " + senderName,
								Status:  "info",
							})
						}
					}
				}
			}
		}()
	}
	return nil
}

func (wc *WebChannel) Stop() error {
	if wc.stopNotify != nil {
		close(wc.stopNotify)
	}
	wc.mu.Lock()
	defer wc.mu.Unlock()
	for conn := range wc.conns {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
	wc.conns = make(map[*websocket.Conn]connInfo)
	return nil
}

// ServeHTTP handles the WebSocket upgrade and message loop.
func (wc *WebChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"127.0.0.1:*", "localhost:*", "192.168.*.*:*", "*"},
	})
	if err != nil {
		wc.logger.Error("websocket accept failed", "error", err)
		return
	}

	// Extract the authenticated user (set by JWT middleware). When no auth
	// context is present (tests, legacy mode), fall back to empty string.
	var userID string
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userID = u.ID
	}

	wc.mu.Lock()
	wc.conns[conn] = connInfo{userID: userID}
	wc.mu.Unlock()

	defer func() {
		wc.mu.Lock()
		delete(wc.conns, conn)
		wc.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()

	// Forward per-connection events (title updates, cross-channel responses) for the connection lifetime.
	connDone := make(chan struct{})
	defer close(connDone)
	if wc.eventBus != nil {
		globalCh := wc.eventBus.Subscribe(bus.SessionTitleSet, bus.MessageResponded, bus.SessionDeleted, bus.SettingsChanged, bus.NotificationsRead)
		go func() {
			for {
				select {
				case <-connDone:
					return
				case evt, ok := <-globalCh:
					if !ok {
						return
					}
					switch evt.Type {
					case bus.SessionTitleSet:
						sk, _ := evt.Payload["session_key"].(string)
						title, _ := evt.Payload["title"].(string)
						wc.writeMessage(ctx, conn, wsMessage{
							Type:       "session_title",
							SessionKey: sk,
							Content:    title,
						})
					case bus.MessageResponded:
						sk, _ := evt.Payload["session_key"].(string)
						// Skip if this connection is currently viewing the same
						// session (the response was already delivered directly via
						// the WS message loop). Other sessions and other clients
						// still receive the update for cross-device sync and
						// sidebar refresh.
						wc.mu.Lock()
						ci := wc.conns[conn]
						wc.mu.Unlock()
						if sk == ci.sessionKey {
							break
						}
						wc.writeMessage(ctx, conn, wsMessage{
							Type:       "session_update",
							SessionKey: sk,
						})
					case bus.SessionDeleted:
						sk, _ := evt.Payload["session_key"].(string)
						wc.writeMessage(ctx, conn, wsMessage{
							Type:       "session_deleted",
							SessionKey: sk,
						})
					case bus.SettingsChanged:
						wc.writeMessage(ctx, conn, wsMessage{
							Type: "settings_changed",
						})
					case bus.NotificationsRead:
						wc.writeMessage(ctx, conn, wsMessage{
							Type: "notifications_read",
						})
					}
				}
			}
		}()
		defer wc.eventBus.Unsubscribe(globalCh)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				return
			}
			wc.logger.Debug("websocket read error", "error", err)
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			wc.writeMessage(ctx, conn, wsMessage{Type: "error", Error: "invalid message format"})
			continue
		}

		sessionKey := msg.SessionKey
		if sessionKey == "" {
			chatID := msg.ChatID
			if chatID == "" {
				chatID = "default"
			}
			sessionKey = "web:" + chatID
		}

		// Track session key for this connection and mark it as the active web session.
		wc.mu.Lock()
		wc.conns[conn] = connInfo{sessionKey: sessionKey, userID: userID}
		wc.mu.Unlock()
		if wc.sessions != nil {
			wc.sessions.SetActiveSession(sessionKey, userID)
		}

		// A "subscribe" message just binds this connection to a session
		// without sending a chat message. Used by the dashboard on connect
		// and session switch so the server knows where to push notifications.
		if msg.Type == "subscribe" {
			continue
		}

		// Cancel an in-flight request for this session.
		if msg.Type == "cancel" {
			wc.mu.Lock()
			if cancelFn, ok := wc.activeReqs[sessionKey]; ok {
				cancelFn()
				delete(wc.activeReqs, sessionKey)
			}
			wc.mu.Unlock()
			continue
		}

		// Cancel an in-flight voice synthesis request.
		if msg.Type == "voice_cancel" {
			wc.eventBus.Publish(bus.Event{
				Type: bus.VoiceCancel,
				Payload: map[string]any{
					"user_id":    userID,
					"message_id": msg.MessageID,
				},
				Timestamp: time.Now(),
			})
			continue
		}

		if msg.Message == "" {
			wc.writeMessage(ctx, conn, wsMessage{Type: "error", Error: "message is required"})
			continue
		}

		// Reject if there is already an in-flight request for this session.
		wc.mu.Lock()
		if _, busy := wc.activeReqs[sessionKey]; busy {
			wc.mu.Unlock()
			wc.writeMessage(ctx, conn, wsMessage{Type: "error", Error: "request already in progress", SessionKey: sessionKey})
			continue
		}
		reqCtx, reqCancel := context.WithCancel(ctx)
		wc.activeReqs[sessionKey] = reqCancel
		wc.mu.Unlock()

		incoming := IncomingMessage{
			Channel:    "web",
			ChatID:     msg.ChatID,
			SessionKey: sessionKey,
			UserID:     userID,
			Text:       msg.Message,
			Private:    msg.Private,
		}

		go func(sessionKey string, incoming IncomingMessage, reqCtx context.Context, reqCancel context.CancelFunc) {
			// Stream activity events while the handler processes the request.
			done := make(chan struct{})
			var stopForward func()
			if wc.eventBus != nil {
				stopForward = wc.forwardActivity(reqCtx, conn, sessionKey, done)
			}

			resp, handlerErr := wc.handler(reqCtx, incoming)
			close(done)
			if stopForward != nil {
				stopForward()
			}

			// Capture cancellation state before cleanup so we can
			// distinguish a user-initiated cancel from a handler error.
			cancelled := reqCtx.Err() != nil

			// Clean up active request BEFORE sending the response so a
			// subsequent message for the same session is not rejected.
			wc.mu.Lock()
			delete(wc.activeReqs, sessionKey)
			wc.mu.Unlock()
			reqCancel()

			if handlerErr != nil {
				if cancelled {
					wc.writeMessage(ctx, conn, wsMessage{
						Type:       "cancelled",
						SessionKey: sessionKey,
					})
					return
				}
				wc.writeMessage(ctx, conn, wsMessage{
					Type:       "error",
					Error:      handlerErr.Error(),
					SessionKey: sessionKey,
				})
				return
			}

			outMsg := wsMessage{
				Type:       "response",
				Content:    resp.Content,
				SessionKey: sessionKey,
			}
			if resp.ToolsUsed != "" {
				outMsg.ToolsUsed = json.RawMessage(resp.ToolsUsed)
			}
			wc.writeMessage(ctx, conn, outMsg)
		}(sessionKey, incoming, reqCtx, reqCancel)
	}
}

// startVoiceSubscriber subscribes to voice audio events and fans them out
// to the owning user's WebSocket connections. Uses a large buffer to absorb
// bursts of audio chunks without blocking the publisher.
func (wc *WebChannel) startVoiceSubscriber(ctx context.Context) {
	ch := wc.eventBus.SubscribeWithBuffer(256, bus.VoiceAudioStart, bus.VoiceAudioChunk, bus.VoiceAudioEnd, bus.VoiceError)
	go func() {
		for {
			select {
			case <-ctx.Done():
				wc.eventBus.Unsubscribe(ch)
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				userID, _ := evt.Payload["user_id"].(string)
				if userID == "" {
					continue
				}
				messageID, _ := evt.Payload["message_id"].(string)
				var msg wsMessage
				switch evt.Type {
				case bus.VoiceAudioStart:
					format, _ := evt.Payload["format"].(string)
					duration, _ := evt.Payload["duration"].(int)
					msg = wsMessage{
						Type:          string(evt.Type),
						MessageID:     messageID,
						VoiceFormat:   format,
						VoiceDuration: duration,
					}
				case bus.VoiceAudioChunk:
					data, _ := evt.Payload["data"].(string)
					msg = wsMessage{
						Type:      string(evt.Type),
						MessageID: messageID,
						VoiceData: data,
					}
				case bus.VoiceAudioEnd:
					msg = wsMessage{
						Type:      string(evt.Type),
						MessageID: messageID,
					}
				case bus.VoiceError:
					errMsg, _ := evt.Payload["error"].(string)
					msg = wsMessage{
						Type:      string(evt.Type),
						MessageID: messageID,
						Error:     errMsg,
					}
				default:
					continue
				}
				wc.sendToUser(userID, msg)
			}
		}
	}()
}

// forwardActivity subscribes to agent activity events and sends them as
// WebSocket status messages until done is closed. Returns a cleanup function.
func (wc *WebChannel) forwardActivity(
	ctx context.Context,
	conn *websocket.Conn,
	sessionKey string,
	done <-chan struct{},
) func() {
	ch := wc.eventBus.Subscribe(bus.AgentThinking, bus.AgentToolCalling, bus.MCPServerStateChanged)

	go func() {
		for {
			select {
			case <-done:
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				// MCP events are global (not session-scoped).
				if evt.Type == bus.MCPServerStateChanged {
					serverName, _ := evt.Payload["server_name"].(string)
					status, _ := evt.Payload["status"].(string)
					errMsg, _ := evt.Payload["error"].(string)
					wc.writeMessage(ctx, conn, wsMessage{
						Type:    "mcp_server_state",
						Content: serverName,
						Status:  status,
						Error:   errMsg,
					})
					continue
				}
				// Only forward agent events for this session.
				if sk, _ := evt.Payload["session_key"].(string); sk != sessionKey {
					continue
				}
				switch evt.Type {
				case bus.AgentThinking:
					wc.writeMessage(ctx, conn, wsMessage{
						Type:       "status",
						SessionKey: sessionKey,
						Status:     "thinking",
					})
				case bus.AgentToolCalling:
					tool, _ := evt.Payload["tool"].(string)
					wc.writeMessage(ctx, conn, wsMessage{
						Type:       "status",
						SessionKey: sessionKey,
						Status:     "tool_calling",
						Tool:       tool,
					})
				}
			}
		}
	}()

	return func() { wc.eventBus.Unsubscribe(ch) }
}

func (wc *WebChannel) writeMessage(ctx context.Context, conn *websocket.Conn, msg wsMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		wc.logger.Error("marshal ws message", "error", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		wc.logger.Debug("websocket write error", "error", err)
	}
}

// broadcast sends a message to all connected WebSocket clients.
func (wc *WebChannel) broadcast(msg wsMessage) {
	wc.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(wc.conns))
	for c := range wc.conns {
		conns = append(conns, c)
	}
	wc.mu.Unlock()

	ctx := context.Background()
	for _, c := range conns {
		wc.writeMessage(ctx, c, msg)
	}
}

// sendToUser sends a message only to WebSocket connections owned by the given user ID.
func (wc *WebChannel) sendToUser(userID string, msg wsMessage) {
	wc.mu.Lock()
	var targets []*websocket.Conn
	for c, info := range wc.conns {
		if info.userID == userID {
			targets = append(targets, c)
		}
	}
	wc.mu.Unlock()

	ctx := context.Background()
	for _, c := range targets {
		wc.writeMessage(ctx, c, msg)
	}
}

// sendToUsers sends a message to connections owned by any of the given user IDs.
func (wc *WebChannel) sendToUsers(userIDs []string, msg wsMessage) {
	set := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		set[id] = true
	}

	wc.mu.Lock()
	var targets []*websocket.Conn
	for c, info := range wc.conns {
		if set[info.userID] {
			targets = append(targets, c)
		}
	}
	wc.mu.Unlock()

	ctx := context.Background()
	for _, c := range targets {
		wc.writeMessage(ctx, c, msg)
	}
}
