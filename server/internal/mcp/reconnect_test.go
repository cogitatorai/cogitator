package mcp

import (
	"context"
	"log/slog"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func TestReconnectBackoff(t *testing.T) {
	delays := []int{1, 2, 4, 8, 16, 30}
	for i, want := range delays {
		got := reconnectDelay(i)
		if int(got.Seconds()) != want {
			t.Errorf("attempt %d: got %v, want %ds", i, got, want)
		}
	}
	// Past max retries should cap at 30s.
	if d := reconnectDelay(10); int(d.Seconds()) != 30 {
		t.Errorf("attempt 10: got %v, want 30s", d)
	}
}

func TestStopServer_CancelsReconnection(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(
		dir+"/mcp.json",
		secretstore.NewFileStore(dir),
		nil,
		slog.Default(),
	)
	m.LoadConfig()
	m.AddServer("test", ServerConfig{URL: "https://invalid.example.com", Transport: "streamable-http"})

	// Simulate a reconnecting state with a cancel func.
	m.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	m.servers["test"].status = StatusReconnecting
	m.servers["test"].reconnectCancel = cancel
	m.mu.Unlock()

	m.StopServer("test")

	// Context should be cancelled.
	if ctx.Err() == nil {
		t.Error("expected reconnection context to be cancelled")
	}

	// Server should be stopped.
	for _, s := range m.Servers() {
		if s.Name == "test" && s.Status != StatusStopped {
			t.Errorf("expected stopped, got %s", s.Status)
		}
	}
}
