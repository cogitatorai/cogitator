package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"nhooyr.io/websocket"
)

func echoHandler(_ context.Context, msg IncomingMessage) (HandlerResponse, error) {
	return HandlerResponse{Content: "Echo: " + msg.Text}, nil
}

func errorHandler(_ context.Context, _ IncomingMessage) (HandlerResponse, error) {
	return HandlerResponse{}, fmt.Errorf("test error")
}

func TestWebChannelName(t *testing.T) {
	wc := NewWebChannel(echoHandler, nil, nil, nil, nil, nil)
	if wc.Name() != "web" {
		t.Errorf("expected 'web', got %q", wc.Name())
	}
}

func TestWebChannelChat(t *testing.T) {
	wc := NewWebChannel(echoHandler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] // http -> ws
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message
	msg := wsMessage{
		Type:    "message",
		Message: "Hello",
		ChatID:  "test-chat",
	}
	data, _ := json.Marshal(msg)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response
	_, respData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp wsMessage
	json.Unmarshal(respData, &resp)

	if resp.Type != "response" {
		t.Errorf("expected type 'response', got %q", resp.Type)
	}
	if resp.Content != "Echo: Hello" {
		t.Errorf("expected 'Echo: Hello', got %q", resp.Content)
	}
	if resp.SessionKey != "web:test-chat" {
		t.Errorf("expected session key 'web:test-chat', got %q", resp.SessionKey)
	}
}

func TestWebChannelEmptyMessage(t *testing.T) {
	wc := NewWebChannel(echoHandler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	msg := wsMessage{Type: "message", Message: ""}
	data, _ := json.Marshal(msg)
	conn.Write(ctx, websocket.MessageText, data)

	_, respData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp wsMessage
	json.Unmarshal(respData, &resp)
	if resp.Type != "error" {
		t.Errorf("expected 'error', got %q", resp.Type)
	}
}

func TestWebChannelHandlerError(t *testing.T) {
	wc := NewWebChannel(errorHandler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	msg := wsMessage{Type: "message", Message: "trigger error"}
	data, _ := json.Marshal(msg)
	conn.Write(ctx, websocket.MessageText, data)

	_, respData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp wsMessage
	json.Unmarshal(respData, &resp)
	if resp.Type != "error" {
		t.Errorf("expected 'error', got %q", resp.Type)
	}
	if resp.Error != "test error" {
		t.Errorf("expected 'test error', got %q", resp.Error)
	}
}

func TestWebChannelInvalidJSON(t *testing.T) {
	wc := NewWebChannel(echoHandler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.Write(ctx, websocket.MessageText, []byte("not json"))

	_, respData, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp wsMessage
	json.Unmarshal(respData, &resp)
	if resp.Type != "error" {
		t.Errorf("expected 'error', got %q", resp.Type)
	}
}

func TestWebChannelDefaultSessionKey(t *testing.T) {
	var capturedSessionKey string
	handler := func(_ context.Context, msg IncomingMessage) (HandlerResponse, error) {
		capturedSessionKey = msg.SessionKey
		return HandlerResponse{Content: "ok"}, nil
	}

	wc := NewWebChannel(handler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// No ChatID or SessionKey set
	msg := wsMessage{Type: "message", Message: "hello"}
	data, _ := json.Marshal(msg)
	conn.Write(ctx, websocket.MessageText, data)

	conn.Read(ctx)

	if capturedSessionKey != "web:default" {
		t.Errorf("expected 'web:default', got %q", capturedSessionKey)
	}
}

func TestWebChannelStop(t *testing.T) {
	wc := NewWebChannel(echoHandler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Stop the channel, which should close all connections
	wc.Stop()

	// Reading should fail since connection was closed
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Error("expected error after stop, got nil")
	}
}

func TestWebChannelMultipleMessages(t *testing.T) {
	callCount := 0
	handler := func(_ context.Context, msg IncomingMessage) (HandlerResponse, error) {
		callCount++
		return HandlerResponse{Content: fmt.Sprintf("Response %d to %s", callCount, msg.Text)}, nil
	}

	wc := NewWebChannel(handler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(http.HandlerFunc(wc.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for i := 1; i <= 3; i++ {
		msg := wsMessage{Type: "message", Message: fmt.Sprintf("msg%d", i), ChatID: "multi"}
		data, _ := json.Marshal(msg)
		conn.Write(ctx, websocket.MessageText, data)

		_, respData, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}

		var resp wsMessage
		json.Unmarshal(respData, &resp)
		expected := fmt.Sprintf("Response %d to msg%d", i, i)
		if resp.Content != expected {
			t.Errorf("round %d: expected %q, got %q", i, expected, resp.Content)
		}
	}
}

func TestWebChannelCancel(t *testing.T) {
	blocked := make(chan struct{})
	handler := func(ctx context.Context, msg IncomingMessage) (HandlerResponse, error) {
		<-ctx.Done()
		close(blocked)
		return HandlerResponse{}, ctx.Err()
	}

	wc := NewWebChannel(handler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(wc)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a chat message that will block until cancelled.
	msg, _ := json.Marshal(wsMessage{Type: "message", Message: "hello", SessionKey: "s1"})
	conn.Write(ctx, websocket.MessageText, msg)

	// Give the handler goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	// Send cancel.
	cancel, _ := json.Marshal(wsMessage{Type: "cancel", SessionKey: "s1"})
	conn.Write(ctx, websocket.MessageText, cancel)

	// Read response: should be "cancelled".
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp wsMessage
	json.Unmarshal(data, &resp)
	if resp.Type != "cancelled" {
		t.Errorf("expected type 'cancelled', got %q (content: %q, error: %q)", resp.Type, resp.Content, resp.Error)
	}
	if resp.SessionKey != "s1" {
		t.Errorf("expected session key 's1', got %q", resp.SessionKey)
	}

	// Wait for handler to actually finish.
	<-blocked
}

func TestWebChannelPrivateFlag(t *testing.T) {
	var received IncomingMessage
	handler := func(_ context.Context, msg IncomingMessage) (HandlerResponse, error) {
		received = msg
		return HandlerResponse{Content: "ok"}, nil
	}

	wc := NewWebChannel(handler, nil, nil, nil, nil, nil)
	server := httptest.NewServer(http.HandlerFunc(wc.ServeHTTP))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx := context.Background()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message with private=true.
	data := []byte(`{"type":"message","message":"secret","chat_id":"priv-chat","private":true}`)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response to ensure the handler ran.
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !received.Private {
		t.Error("expected Private=true on IncomingMessage, got false")
	}
	if received.Text != "secret" {
		t.Errorf("expected Text=%q, got %q", "secret", received.Text)
	}
}

func TestWebChannelBroadcastNotification(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	notifStore := notification.NewStore(db)
	taskStore := task.NewStore(db)
	taskID, err := taskStore.CreateTask(&task.Task{
		Name: "broadcast-task", Prompt: "test", Broadcast: true,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	wc := NewWebChannel(echoHandler, eventBus, nil, notifStore, nil, nil)
	wc.SetUserIDsFunc(func() []string {
		return []string{"user-alice", "user-bob", "user-charlie"}
	})
	if err := wc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer wc.Stop()

	// Publish a broadcast task notification.
	eventBus.Publish(bus.Event{
		Type: bus.TaskNotifyChat,
		Payload: map[string]any{
			"task_id":   taskID,
			"task_name": "broadcast-task",
			"run_id":    int64(10),
			"result":    "all done",
			"user_id":   "user-alice",
			"trigger":   "cron",
			"broadcast": true,
		},
	})

	// Give the event loop time to process.
	time.Sleep(50 * time.Millisecond)

	// Each user should have received a notification.
	for _, uid := range []string{"user-alice", "user-bob", "user-charlie"} {
		notifs, total, err := notifStore.List(uid, 10, 0)
		if err != nil {
			t.Fatalf("list notifications for %s: %v", uid, err)
		}
		if total != 1 {
			t.Errorf("expected 1 notification for %s, got %d", uid, total)
		}
		if len(notifs) == 1 && notifs[0].TaskName != "broadcast-task" {
			t.Errorf("expected task_name 'broadcast-task' for %s, got %q", uid, notifs[0].TaskName)
		}
	}
}

func TestWebChannelNonBroadcastNotification(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	notifStore := notification.NewStore(db)
	taskStore := task.NewStore(db)
	taskID, err := taskStore.CreateTask(&task.Task{
		Name: "private-task", Prompt: "test",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	wc := NewWebChannel(echoHandler, eventBus, nil, notifStore, nil, nil)
	wc.SetUserIDsFunc(func() []string {
		return []string{"user-alice", "user-bob"}
	})
	if err := wc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer wc.Stop()

	// Publish a non-broadcast task notification.
	eventBus.Publish(bus.Event{
		Type: bus.TaskNotifyChat,
		Payload: map[string]any{
			"task_id":   taskID,
			"task_name": "private-task",
			"run_id":    int64(20),
			"result":    "done",
			"user_id":   "user-alice",
			"trigger":   "manual",
			"broadcast": false,
		},
	})

	time.Sleep(50 * time.Millisecond)

	// Only the task owner should have a notification.
	notifs, total, err := notifStore.List("user-alice", 10, 0)
	if err != nil {
		t.Fatalf("list alice: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 notification for alice, got %d", total)
	}
	if len(notifs) == 1 && notifs[0].TaskName != "private-task" {
		t.Errorf("expected task_name 'private-task', got %q", notifs[0].TaskName)
	}

	// Bob should have no notifications.
	_, bobTotal, err := notifStore.List("user-bob", 10, 0)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	if bobTotal != 0 {
		t.Errorf("expected 0 notifications for bob, got %d", bobTotal)
	}
}
