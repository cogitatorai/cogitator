//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegration_EverythingServer(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not found, skipping integration test")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")

	secPath := filepath.Join(dir, "secrets.yaml")
	m := NewManager(cfgPath, secPath, nil, slog.Default())
	if err := m.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}

	if err := m.AddServer("everything", ServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
	}); err != nil {
		t.Fatalf("add server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start and discover tools.
	if err := m.StartServer(ctx, "everything"); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer m.StopAll()

	tools, err := m.Tools(ctx, "everything")
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool from the everything server")
	}
	t.Logf("discovered %d tools", len(tools))
	for _, tool := range tools {
		t.Logf("  %s: %s", tool.QualifiedName, tool.Description)
	}

	// Find the echo tool.
	var echoTool string
	for _, tool := range tools {
		if tool.Name == "echo" {
			echoTool = tool.Name
			break
		}
	}
	if echoTool == "" {
		t.Fatalf("echo tool not found among %d tools", len(tools))
	}

	// Call the echo tool.
	result, err := m.CallTool(ctx, "everything", echoTool, json.RawMessage(`{"message":"hello from cogitator"}`))
	if err != nil {
		t.Fatalf("call echo: %v", err)
	}
	t.Logf("echo result: %s", result)
	if result == "" {
		t.Error("expected non-empty echo result")
	}

	// Verify server status.
	for _, s := range m.Servers() {
		if s.Name == "everything" {
			if s.Status != StatusRunning {
				t.Errorf("expected status %q, got %q", StatusRunning, s.Status)
			}
			if s.ToolCount == 0 {
				t.Error("expected non-zero tool count")
			}
			break
		}
	}

	// Stop and verify.
	if err := m.StopServer("everything"); err != nil {
		t.Fatalf("stop server: %v", err)
	}
	for _, s := range m.Servers() {
		if s.Name == "everything" && s.Status != StatusStopped {
			t.Errorf("expected status %q after stop, got %q", StatusStopped, s.Status)
		}
	}
}

func TestIntegration_RemoteConfigValidation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	secPath := filepath.Join(dir, "secrets.yaml")

	m := NewManager(cfgPath, secPath, nil, slog.Default())
	if err := m.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Add a remote server.
	err := m.AddServer("remote", ServerConfig{
		URL:       "https://mcp.example.com/v1",
		Transport: "streamable-http",
		Headers:   map[string]string{"X-Test": "value"},
	})
	if err != nil {
		t.Fatalf("add remote server: %v", err)
	}

	// Verify it's stored correctly.
	var found ServerStatus
	for _, s := range m.Servers() {
		if s.Name == "remote" {
			found = s
			break
		}
	}
	if !found.Remote {
		t.Error("expected remote=true")
	}
	if found.URL != "https://mcp.example.com/v1" {
		t.Errorf("url = %q", found.URL)
	}
	if found.Transport != "streamable-http" {
		t.Errorf("transport = %q", found.Transport)
	}

	// Verify config persisted with remote fields.
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	sc := cfg.Servers["remote"]
	if sc.URL != "https://mcp.example.com/v1" {
		t.Errorf("persisted url = %q", sc.URL)
	}
	if sc.Command != "" {
		t.Errorf("remote should not have command, got %q", sc.Command)
	}

	// Save secrets for the remote server.
	err = m.SaveServerSecrets("remote", &ServerSecrets{
		Headers: map[string]string{"Authorization": "Bearer test"},
		OAuth: &OAuthSecrets{
			ClientID:     "cid",
			ClientSecret: "csec",
			Scopes:       []string{"tools:read"},
		},
	})
	if err != nil {
		t.Fatalf("save secrets: %v", err)
	}

	// Reload and verify secrets.
	secrets, err := LoadMCPSecrets(secPath)
	if err != nil {
		t.Fatalf("load secrets: %v", err)
	}
	sec := secrets["remote"]
	if sec == nil {
		t.Fatal("expected secrets for remote")
	}
	if sec.Headers["Authorization"] != "Bearer test" {
		t.Errorf("auth header = %q", sec.Headers["Authorization"])
	}
	if sec.OAuth == nil || sec.OAuth.ClientID != "cid" {
		t.Error("OAuth mismatch")
	}
}
