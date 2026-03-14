package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// cdpRequest is the shape of every CDP message sent by the client.
type cdpRequest struct {
	ID        int             `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId"`
}

// mockCDPServer sets up a combined HTTP + WebSocket server that emulates
// the Chrome DevTools Protocol discovery and command endpoints.
type mockCDPServer struct {
	srv       *httptest.Server
	targetID  string
	sessionID string

	// connMu guards activeConn.
	connMu     sync.Mutex
	activeConn *websocket.Conn
}

// newMockCDPServer constructs and starts the mock server. The caller must
// call srv.Close() when done.
func newMockCDPServer(t *testing.T) *mockCDPServer {
	t.Helper()

	m := &mockCDPServer{
		targetID:  "AABBCCDD11223344",
		sessionID: "test-session",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/devtools/browser", m.handleWebSocket)

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// wsURL returns the WebSocket URL that clients should connect to.
func (m *mockCDPServer) wsURL() string {
	return "ws" + strings.TrimPrefix(m.srv.URL, "http") + "/devtools/browser"
}

func (m *mockCDPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(32 * 1024 * 1024)

	m.connMu.Lock()
	m.activeConn = conn
	m.connMu.Unlock()

	defer func() {
		m.connMu.Lock()
		if m.activeConn == conn {
			m.activeConn = nil
		}
		m.connMu.Unlock()
		conn.CloseNow()
	}()

	ctx := r.Context()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var req cdpRequest
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}

		result := m.dispatchCDP(req)

		resp, _ := json.Marshal(map[string]any{
			"id":     req.ID,
			"result": result,
		})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			return
		}
	}
}

// closeActiveConn sends a normal-closure WebSocket close frame to the client,
// causing the client's readLoop to receive an error and call onClose.
func (m *mockCDPServer) closeActiveConn() {
	m.connMu.Lock()
	conn := m.activeConn
	m.connMu.Unlock()
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "server closing")
	}
}

// dispatchCDP returns the result object for a given CDP command.
func (m *mockCDPServer) dispatchCDP(req cdpRequest) any {
	switch req.Method {
	case "Target.attachToTarget":
		return map[string]string{"sessionId": m.sessionID}

	case "Target.getTargets":
		return map[string]any{
			"targetInfos": []map[string]string{
				{"targetId": m.targetID, "type": "page", "title": "Mock Page", "url": "https://example.com"},
			},
		}

	case "Target.createTarget":
		var p struct{ URL string `json:"url"` }
		json.Unmarshal(req.Params, &p)
		return map[string]string{"targetId": "NEWTARGET12345678"}

	case "Target.closeTarget":
		return map[string]bool{"success": true}

	case "Browser.getVersion":
		return map[string]string{"product": "Chrome/125.0.0.0 (Mock)"}

	case "Page.navigate":
		return map[string]string{"frameId": "frame-1"}

	case "Page.captureScreenshot":
		// Minimal 1x1 white PNG, base64-encoded.
		pngBytes := minimalPNG()
		return map[string]string{"data": base64.StdEncoding.EncodeToString(pngBytes)}

	case "Accessibility.getFullAXTree":
		nodes := []AXNode{
			{NodeID: "1", Role: AXValue{Value: "RootWebArea"}, Name: AXValue{Value: "Mock Page"}},
			{NodeID: "2", ParentID: "1", Role: AXValue{Value: "heading"}, Name: AXValue{Value: "Hello World"}},
			{NodeID: "3", ParentID: "1", Role: AXValue{Value: "button"}, Name: AXValue{Value: "Click me"}},
		}
		return map[string]any{"nodes": nodes}

	case "Input.dispatchMouseEvent", "Input.insertText", "Input.dispatchKeyEvent":
		return map[string]any{}

	case "Runtime.evaluate":
		return m.handleRuntimeEval(req.Params)

	default:
		return map[string]any{}
	}
}

// handleRuntimeEval interprets the expression in the Runtime.evaluate params
// and returns an appropriate stubbed result.
func (m *mockCDPServer) handleRuntimeEval(params json.RawMessage) any {
	var p struct {
		Expression string `json:"expression"`
	}
	json.Unmarshal(params, &p)
	expr := p.Expression

	switch {
	case expr == "document.readyState":
		return map[string]any{
			"result": map[string]any{"type": "string", "value": "complete"},
		}

	case expr == "window.devicePixelRatio":
		return map[string]any{
			"result": map[string]any{"type": "number", "value": 1.0},
		}

	case strings.Contains(expr, "outerHTML"):
		return map[string]any{
			"result": map[string]any{"type": "string", "value": "<html><body>Mock</body></html>"},
		}

	case strings.Contains(expr, "scrollBy"):
		return map[string]any{
			"result": map[string]any{"type": "undefined"},
		}

	case strings.Contains(expr, "scrollIntoView") && strings.Contains(expr, "click"):
		// browser_click JS: returns JSON string
		clickResult, _ := json.Marshal(map[string]any{
			"ok":   true,
			"tag":  "BUTTON",
			"text": "Click me",
		})
		return map[string]any{
			"result": map[string]any{"type": "string", "value": string(clickResult)},
		}

	case strings.Contains(expr, "performance.getEntriesByType"):
		entries, _ := json.Marshal([]map[string]any{
			{"name": "https://example.com/app.js", "duration": 120, "size": 4096, "type": "script"},
		})
		return map[string]any{
			"result": map[string]any{"type": "string", "value": string(entries)},
		}

	default:
		// Generic eval: echo back a string result
		return map[string]any{
			"result": map[string]any{"type": "string", "value": fmt.Sprintf("eval:%s", expr)},
		}
	}
}

// minimalPNG returns a valid 1x1 white PNG as raw bytes.
func minimalPNG() []byte {
	// This is a real, minimal 1x1 white PNG.
	b64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwADhQGAWjR9awAAAABJRU5ErkJggg=="
	data, _ := base64.StdEncoding.DecodeString(b64)
	return data
}

// setupIntegration creates a mock server and a Connector wired to it.
func setupIntegration(t *testing.T) (*mockCDPServer, *Connector) {
	t.Helper()
	mock := newMockCDPServer(t)

	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	// Bypass DiscoverWSURL (which would find real Chrome) and point at mock.
	c.wsURLOverride = mock.wsURL()
	c.pollInterval = 100 * time.Millisecond
	return mock, c
}

// TestIntegration is the end-to-end test covering the full connector lifecycle.
func TestIntegration(t *testing.T) {
	_, c := setupIntegration(t)

	// Step a: Enable the connector.
	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	t.Cleanup(func() { c.Disable() })

	// Step b: Verify enabled and connected.
	if !c.IsEnabled() {
		t.Fatal("connector should be enabled")
	}
	if !c.IsConnected() {
		t.Fatal("connector should be connected to mock server")
	}

	st := c.Status()
	if st.ChromeVersion == "" {
		t.Error("Status.ChromeVersion should be populated")
	}
	if !strings.Contains(st.ChromeVersion, "Chrome") {
		t.Errorf("ChromeVersion should contain 'Chrome', got %q", st.ChromeVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step c: browser_list
	t.Run("browser_list", func(t *testing.T) {
		out, err := c.Execute(ctx, "browser_list", "")
		if err != nil {
			t.Fatalf("browser_list: %v", err)
		}
		if out == "" || out == "No open tabs." {
			t.Fatalf("expected non-empty tab list, got: %q", out)
		}
		if !strings.Contains(out, "Mock Page") {
			t.Errorf("expected 'Mock Page' in list output, got:\n%s", out)
		}
		if !strings.Contains(out, "https://example.com") {
			t.Errorf("expected URL in list output, got:\n%s", out)
		}
	})

	// Derive a short target prefix from the mock target ID.
	targetPrefix := "AABBCCDD"

	// Step d: browser_navigate
	t.Run("browser_navigate", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"target": targetPrefix,
			"url":    "https://example.com/test",
		})
		out, err := c.Execute(ctx, "browser_navigate", string(args))
		if err != nil {
			t.Fatalf("browser_navigate: %v", err)
		}
		if !strings.Contains(out, "https://example.com/test") {
			t.Errorf("expected URL in navigate output, got: %q", out)
		}
	})

	// Step e: browser_snapshot
	t.Run("browser_snapshot", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{"target": targetPrefix})
		out, err := c.Execute(ctx, "browser_snapshot", string(args))
		if err != nil {
			t.Fatalf("browser_snapshot: %v", err)
		}
		if !strings.Contains(out, "[RootWebArea]") {
			t.Errorf("expected accessibility tree in output, got:\n%s", out)
		}
		if !strings.Contains(out, "Mock Page") {
			t.Errorf("expected 'Mock Page' in snapshot output, got:\n%s", out)
		}
		if !strings.Contains(out, "[button]") {
			t.Errorf("expected button node in snapshot output, got:\n%s", out)
		}
	})

	// Step f: browser_html
	t.Run("browser_html", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{"target": targetPrefix})
		out, err := c.Execute(ctx, "browser_html", string(args))
		if err != nil {
			t.Fatalf("browser_html: %v", err)
		}
		if !strings.Contains(out, "<html>") {
			t.Errorf("expected HTML content, got: %q", out)
		}
	})

	// Step g: browser_click
	t.Run("browser_click", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"target":   targetPrefix,
			"selector": "button",
		})
		out, err := c.Execute(ctx, "browser_click", string(args))
		if err != nil {
			t.Fatalf("browser_click: %v", err)
		}
		if !strings.Contains(out, "Clicked") {
			t.Errorf("expected 'Clicked' in output, got: %q", out)
		}
	})

	// Step h: browser_type
	t.Run("browser_type", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"target": targetPrefix,
			"text":   "hello world",
		})
		out, err := c.Execute(ctx, "browser_type", string(args))
		if err != nil {
			t.Fatalf("browser_type: %v", err)
		}
		if !strings.Contains(out, "Typed") {
			t.Errorf("expected 'Typed' in output, got: %q", out)
		}
		if !strings.Contains(out, "11") {
			t.Errorf("expected character count 11 in output, got: %q", out)
		}
	})

	// Step i: browser_eval
	t.Run("browser_eval", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"target":     targetPrefix,
			"expression": "document.title",
		})
		out, err := c.Execute(ctx, "browser_eval", string(args))
		if err != nil {
			t.Fatalf("browser_eval: %v", err)
		}
		if out == "" {
			t.Error("browser_eval returned empty result")
		}
	})

	// Step j: browser_scroll
	t.Run("browser_scroll", func(t *testing.T) {
		args, _ := json.Marshal(map[string]string{
			"target":    targetPrefix,
			"direction": "down",
		})
		out, err := c.Execute(ctx, "browser_scroll", string(args))
		if err != nil {
			t.Fatalf("browser_scroll: %v", err)
		}
		if !strings.Contains(out, "Scrolled") {
			t.Errorf("expected 'Scrolled' in output, got: %q", out)
		}
	})

	// Step k: Disable and verify cleanup.
	t.Run("disable", func(t *testing.T) {
		if err := c.Disable(); err != nil {
			t.Fatalf("Disable: %v", err)
		}
		if c.IsEnabled() {
			t.Error("connector should be disabled after Disable()")
		}
		if c.IsConnected() {
			t.Error("connector should not be connected after Disable()")
		}
		// Executing any tool should fail cleanly.
		args, _ := json.Marshal(map[string]string{"target": targetPrefix})
		_, err := c.Execute(ctx, "browser_list", string(args))
		if err == nil {
			t.Error("expected error when executing on disabled connector")
		}
	})
}

// TestIntegrationStatus verifies Status() reports correct fields after connect.
func TestIntegrationStatus(t *testing.T) {
	mock, c := setupIntegration(t)

	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	defer c.Disable()

	st := c.Status()
	if !st.Enabled {
		t.Error("Status.Enabled should be true")
	}
	if !st.Connected {
		t.Error("Status.Connected should be true")
	}
	_ = mock // mock used by setupIntegration
	if st.Error != "" {
		t.Errorf("Status.Error should be empty after successful connect, got %q", st.Error)
	}
}

// TestIntegrationDisconnectAfterServerClose verifies that when the mock server
// closes, the connector marks itself as disconnected.
func TestIntegrationDisconnectAfterServerClose(t *testing.T) {
	mock, c := setupIntegration(t)

	if err := c.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	defer c.Disable()

	if !c.IsConnected() {
		t.Fatal("should be connected before server close")
	}

	// Send a proper WebSocket close frame. This causes the client's readLoop
	// to receive an error immediately and call the onClose handler which sets
	// c.connected = false.
	mock.closeActiveConn()

	// Poll up to 2 seconds for the onClose callback to propagate.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !c.IsConnected() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Error("connector should detect disconnection after WebSocket close")
}
