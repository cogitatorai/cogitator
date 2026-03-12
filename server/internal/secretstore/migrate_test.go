package secretstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func TestMigrateConnectorTokens(t *testing.T) {
	dir := t.TempDir()

	yaml := `connectors:
  google:
    user1:
      access_token: "at1"
      refresh_token: "rt1"
      token_type: "Bearer"
      expiry: "2026-03-06T12:00:00Z"
      client_id: "cid"
      client_secret: "csec"
`
	src := filepath.Join(dir, "connector_tokens.yaml")
	if err := os.WriteFile(src, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	store := secretstore.NewFileStore(filepath.Join(dir, "store"))
	if err := secretstore.MigrateFiles(store, dir); err != nil {
		t.Fatalf("MigrateFiles: %v", err)
	}

	// Verify token stored under namespace "connector", key "google:user1"
	val, err := store.Get("connector", "google:user1")
	if err != nil {
		t.Fatalf("Get connector google:user1: %v", err)
	}

	var tok map[string]any
	if err := json.Unmarshal([]byte(val), &tok); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	if tok["access_token"] != "at1" {
		t.Errorf("access_token = %v, want at1", tok["access_token"])
	}
	if tok["refresh_token"] != "rt1" {
		t.Errorf("refresh_token = %v, want rt1", tok["refresh_token"])
	}
	if tok["client_id"] != "cid" {
		t.Errorf("client_id = %v, want cid", tok["client_id"])
	}

	// Verify .bak exists and original is gone
	bak := src + ".bak"
	if _, err := os.Stat(bak); err != nil {
		t.Errorf("expected .bak file to exist: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("expected original file to be gone")
	}
}

func TestMigrateSecrets(t *testing.T) {
	dir := t.TempDir()

	yaml := `providers:
  openai:
    api_key: "sk-123"
channels:
  telegram:
    bot_token: "tg_bot_abc"
github:
  token: "ghp_abc123"
relay:
  token: "relay_xyz"
mcp:
  myserver:
    headers:
      Authorization: "Bearer mcp_token"
`
	src := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(src, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	store := secretstore.NewFileStore(filepath.Join(dir, "store"))
	if err := secretstore.MigrateFiles(store, dir); err != nil {
		t.Fatalf("MigrateFiles: %v", err)
	}

	checks := []struct {
		ns, key, want string
	}{
		{"app", "github_token", "ghp_abc123"},
		{"app", "relay_token", "relay_xyz"},
		{"app", "provider:openai", "sk-123"},
		{"app", "telegram_bot_token", "tg_bot_abc"},
	}
	for _, c := range checks {
		got, err := store.Get(c.ns, c.key)
		if err != nil {
			t.Errorf("Get(%q, %q): %v", c.ns, c.key, err)
			continue
		}
		if got != c.want {
			t.Errorf("Get(%q, %q) = %q, want %q", c.ns, c.key, got, c.want)
		}
	}

	// Verify MCP server stored as JSON
	mcpVal, err := store.Get("mcp", "myserver")
	if err != nil {
		t.Fatalf("Get mcp myserver: %v", err)
	}
	var mcpObj map[string]any
	if err := json.Unmarshal([]byte(mcpVal), &mcpObj); err != nil {
		t.Fatalf("unmarshal mcp value: %v", err)
	}
	headers, ok := mcpObj["headers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp headers not a map, got %T", mcpObj["headers"])
	}
	if headers["Authorization"] != "Bearer mcp_token" {
		t.Errorf("Authorization = %v, want 'Bearer mcp_token'", headers["Authorization"])
	}

	// Verify .bak exists and original is gone
	bak := src + ".bak"
	if _, err := os.Stat(bak); err != nil {
		t.Errorf("expected .bak file to exist: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("expected original file to be gone")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()

	yaml := `github:
  token: "ghp_first"
`
	src := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(src, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	store := secretstore.NewFileStore(filepath.Join(dir, "store"))

	// First migration
	if err := secretstore.MigrateFiles(store, dir); err != nil {
		t.Fatalf("first MigrateFiles: %v", err)
	}

	// Overwrite the value in the store
	if err := store.Set("app", "github_token", "ghp_overwritten"); err != nil {
		t.Fatal(err)
	}

	// Write a new secrets.yaml (migration should skip because .bak exists)
	newYAML := `github:
  token: "ghp_second"
`
	if err := os.WriteFile(src, []byte(newYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	// Second migration: should be a no-op because .bak exists
	if err := secretstore.MigrateFiles(store, dir); err != nil {
		t.Fatalf("second MigrateFiles: %v", err)
	}

	// Overwritten value should be preserved
	got, err := store.Get("app", "github_token")
	if err != nil {
		t.Fatalf("Get github_token: %v", err)
	}
	if got != "ghp_overwritten" {
		t.Errorf("github_token = %q, want %q", got, "ghp_overwritten")
	}
}

func TestMigrateNoFiles(t *testing.T) {
	dir := t.TempDir()
	store := secretstore.NewFileStore(filepath.Join(dir, "store"))

	if err := secretstore.MigrateFiles(store, dir); err != nil {
		t.Fatalf("MigrateFiles on empty dir: %v", err)
	}
}
