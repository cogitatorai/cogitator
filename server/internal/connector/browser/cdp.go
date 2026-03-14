package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

const defaultTimeout = 15 * time.Second

// Client is a lightweight Chrome DevTools Protocol client.
type Client struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	nextID    int
	pending   map[int]chan cdpResponse
	closeOnce sync.Once
	done      chan struct{}
	timeout   time.Duration
	onClose   func()
}

type cdpResponse struct {
	Result json.RawMessage
	Error  *cdpError
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewClient creates a CDP client.
func NewClient() *Client {
	return &Client{
		pending: make(map[int]chan cdpResponse),
		done:    make(chan struct{}),
		timeout: defaultTimeout,
	}
}

// OnClose registers a callback for when the WebSocket closes.
// Must be called before Connect; not safe to call concurrently.
func (c *Client) OnClose(fn func()) {
	c.onClose = fn
}

// Connect establishes a WebSocket connection to Chrome.
func (c *Client) Connect(ctx context.Context, wsURL string) error {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("CDP connect: %w", err)
	}
	conn.SetReadLimit(32 * 1024 * 1024) // 32MB for large responses
	c.conn = conn
	go c.readLoop()
	return nil
}

// Send sends a CDP command and waits for the response.
func (c *Client) Send(ctx context.Context, method string, params any, sessionID string) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}

	data, err := json.Marshal(msg)
	if err != nil {
		c.removePending(id)
		return nil, err
	}

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("CDP write: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("CDP error: %s", resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(c.timeout):
		c.removePending(id)
		return nil, fmt.Errorf("CDP timeout: %s", method)
	case <-c.done:
		return nil, fmt.Errorf("CDP connection closed")
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	}
}

// AttachToTarget attaches to a page target and returns the session ID.
func (c *Client) AttachToTarget(ctx context.Context, targetID string) (string, error) {
	result, err := c.Send(ctx, "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, "")
	if err != nil {
		return "", err
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// Close closes the WebSocket connection.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.conn != nil {
			c.conn.Close(websocket.StatusNormalClosure, "")
		}
		c.drainPending()
	})
}

func (c *Client) readLoop() {
	defer func() {
		c.Close()
		if c.onClose != nil {
			c.onClose()
		}
	}()
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *cdpError       `json:"error"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID > 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- cdpResponse{Result: msg.Result, Error: msg.Error}
			}
		}
	}
}

func (c *Client) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) drainPending() {
	c.mu.Lock()
	for id, ch := range c.pending {
		ch <- cdpResponse{Error: &cdpError{Message: "connection closed"}}
		delete(c.pending, id)
	}
	c.mu.Unlock()
}
