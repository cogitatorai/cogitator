package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestClientSendReceive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		for {
			_, msg, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var req struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			json.Unmarshal(msg, &req)
			resp, _ := json.Marshal(map[string]any{
				"id":     req.ID,
				"result": map[string]string{"method": req.Method},
			})
			c.Write(r.Context(), websocket.MessageText, resp)
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client := NewClient()
	ctx := context.Background()
	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	result, err := client.Send(ctx, "Target.getTargets", nil, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got struct{ Method string }
	json.Unmarshal(result, &got)
	if got.Method != "Target.getTargets" {
		t.Errorf("expected echoed method, got %q", got.Method)
	}
}

func TestClientSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		time.Sleep(30 * time.Second)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client := NewClient()
	client.timeout = 100 * time.Millisecond
	ctx := context.Background()
	client.Connect(ctx, wsURL)
	defer client.Close()

	_, err := client.Send(ctx, "Page.navigate", nil, "")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
