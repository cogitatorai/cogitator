package browser

import (
	"log/slog"
	"testing"
	"time"
)

func TestNewConnector(t *testing.T) {
	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	if c.IsEnabled() {
		t.Error("should not be enabled by default")
	}
	if c.config.Port != 9222 {
		t.Errorf("expected default port 9222, got %d", c.config.Port)
	}
}

func TestConnectorEnableNoChrome(t *testing.T) {
	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	// Point at an unreachable WS URL so we don't discover real Chrome.
	c.wsURLOverride = "ws://127.0.0.1:19876/devtools/browser/fake"
	c.pollInterval = 100 * time.Millisecond

	err := c.Enable()
	// Enable should succeed even if Chrome isn't reachable.
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !c.IsEnabled() {
		t.Error("should be enabled")
	}
	if c.IsConnected() {
		t.Error("should not be connected without Chrome")
	}
	c.Disable()
}

func TestConnectorDisable(t *testing.T) {
	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	c.wsURLOverride = "ws://127.0.0.1:19877/devtools/browser/fake"
	c.pollInterval = 100 * time.Millisecond

	c.Enable()
	if !c.IsEnabled() {
		t.Error("should be enabled after Enable()")
	}
	c.Disable()
	if c.IsEnabled() {
		t.Error("should not be enabled after Disable()")
	}
}

func TestConnectorConfigPersistence(t *testing.T) {
	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	c.config.Port = 9333
	if err := c.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	c2 := NewConnector(dir, slog.Default())
	if c2.config.Port != 9333 {
		t.Errorf("expected port 9333, got %d", c2.config.Port)
	}
}

func TestConnectorOnToolsChanged(t *testing.T) {
	dir := t.TempDir()
	c := NewConnector(dir, slog.Default())
	c.wsURLOverride = "ws://127.0.0.1:19878/devtools/browser/fake"
	c.pollInterval = 100 * time.Millisecond

	called := 0
	c.OnToolsChanged(func() {
		called++
	})

	c.Enable()
	if called != 1 {
		t.Errorf("expected 1 callback on Enable, got %d", called)
	}
	c.Disable()
	if called != 2 {
		t.Errorf("expected 2 callbacks total after Disable, got %d", called)
	}
}
