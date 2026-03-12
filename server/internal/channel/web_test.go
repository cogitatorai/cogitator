package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
