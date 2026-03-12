package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func TestExtractApplySecretsRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.SetProviderAPIKey("openai", "sk-openai-test")
	cfg.SetProviderAPIKey("anthropic", "sk-ant-test")
	cfg.Channels.Telegram.BotToken = "bot-token-123"

	sec := ExtractSecrets(cfg)

	if sec.Providers["openai"].APIKey != "sk-openai-test" {
		t.Errorf("openai key = %q, want 'sk-openai-test'", sec.Providers["openai"].APIKey)
	}
	if sec.Providers["anthropic"].APIKey != "sk-ant-test" {
		t.Errorf("anthropic key = %q, want 'sk-ant-test'", sec.Providers["anthropic"].APIKey)
	}
	if sec.Channels.Telegram.BotToken != "bot-token-123" {
		t.Errorf("bot token = %q, want 'bot-token-123'", sec.Channels.Telegram.BotToken)
	}

	// Apply to a fresh config.
	fresh := Default()
	ApplySecrets(fresh, sec)

	if fresh.ProviderAPIKey("openai") != "sk-openai-test" {
		t.Errorf("after apply, openai key = %q", fresh.ProviderAPIKey("openai"))
	}
	if fresh.Channels.Telegram.BotToken != "bot-token-123" {
		t.Errorf("after apply, bot token = %q", fresh.Channels.Telegram.BotToken)
	}
}

func TestSecretsExcludedFromYAML(t *testing.T) {
	cfg := Default()
	cfg.SetProviderAPIKey("openai", "sk-secret")
	cfg.Channels.Telegram.BotToken = "bot-secret"

	sec := ExtractSecrets(cfg)
	clearSecrets(cfg)
	data, err := yaml.Marshal(cfg)
	ApplySecrets(cfg, sec)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	s := string(data)
	if containsAny(s, "sk-secret", "bot-secret") {
		t.Errorf("yaml output contains secrets:\n%s", s)
	}

	// Verify in-memory values were restored.
	if cfg.ProviderAPIKey("openai") != "sk-secret" {
		t.Error("in-memory APIKey not restored after clearSecrets/ApplySecrets")
	}
	if cfg.Channels.Telegram.BotToken != "bot-secret" {
		t.Error("in-memory BotToken not restored after clearSecrets/ApplySecrets")
	}
}

func TestSecretsFilePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.yaml")

	original := SecretsData{
		Providers: map[string]ProviderSecret{
			"openai": {APIKey: "sk-test"},
		},
		Channels: ChannelSecrets{
			Telegram: TelegramSecret{BotToken: "bot-test"},
		},
	}

	if err := SaveSecrets(path, original); err != nil {
		t.Fatalf("SaveSecrets error: %v", err)
	}

	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}

	loaded, err := LoadSecrets(path)
	if err != nil {
		t.Fatalf("LoadSecrets error: %v", err)
	}
	if loaded.Providers["openai"].APIKey != "sk-test" {
		t.Errorf("loaded APIKey = %q, want 'sk-test'", loaded.Providers["openai"].APIKey)
	}
	if loaded.Channels.Telegram.BotToken != "bot-test" {
		t.Errorf("loaded BotToken = %q, want 'bot-test'", loaded.Channels.Telegram.BotToken)
	}
}

func TestLoadSecretsMissingFile(t *testing.T) {
	sec, err := LoadSecrets("/nonexistent/secrets.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(sec.Providers) != 0 {
		t.Errorf("expected empty providers, got %v", sec.Providers)
	}
}

func TestStoreSaveSplitsSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cogitator.yaml")

	ss := secretstore.NewFileStore(dir)

	cfg := Default()
	cfg.SetProviderAPIKey("openai", "sk-split-test")
	cfg.Channels.Telegram.BotToken = "bot-split-test"

	store := NewStore(cfg, cfgPath, ss)
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// Main config should NOT contain secrets.
	mainData, _ := os.ReadFile(cfgPath)
	if containsAny(string(mainData), "sk-split-test", "bot-split-test") {
		t.Errorf("main config contains secrets:\n%s", string(mainData))
	}

	// Secrets should be readable back from the store.
	loaded, err := LoadSecretsFromStore(ss)
	if err != nil {
		t.Fatalf("LoadSecretsFromStore error: %v", err)
	}
	if loaded.Providers["openai"].APIKey != "sk-split-test" {
		t.Errorf("store missing API key, got %q", loaded.Providers["openai"].APIKey)
	}
	if loaded.Channels.Telegram.BotToken != "bot-split-test" {
		t.Errorf("store missing bot token, got %q", loaded.Channels.Telegram.BotToken)
	}

	// In-memory config should still have the secrets.
	got := store.Get()
	if got.ProviderAPIKey("openai") != "sk-split-test" {
		t.Error("in-memory APIKey lost after Save")
	}
	if got.Channels.Telegram.BotToken != "bot-split-test" {
		t.Error("in-memory BotToken lost after Save")
	}
}

func TestSaveSecretsPreservesUnmanagedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.yaml")

	// Pre-populate with a GitHub token and an MCP section.
	existing := `providers:
    openai:
        api_key: sk-old
github:
    token: ghp_my_secret_token
mcp:
    my-server:
        headers:
            Authorization: Bearer mcp-secret
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	// Save with a new provider key (simulating a settings update).
	sec := SecretsData{
		Providers: map[string]ProviderSecret{
			"openai": {APIKey: "sk-new"},
		},
	}
	if err := SaveSecrets(path, sec); err != nil {
		t.Fatalf("SaveSecrets error: %v", err)
	}

	// Load and verify the GitHub token survived.
	loaded, err := LoadSecrets(path)
	if err != nil {
		t.Fatalf("LoadSecrets error: %v", err)
	}
	if loaded.GitHub.Token != "ghp_my_secret_token" {
		t.Errorf("GitHub token = %q, want 'ghp_my_secret_token'", loaded.GitHub.Token)
	}
	if loaded.Providers["openai"].APIKey != "sk-new" {
		t.Errorf("provider key = %q, want 'sk-new'", loaded.Providers["openai"].APIKey)
	}

	// Verify MCP section survived by reading raw YAML.
	raw, _ := os.ReadFile(path)
	if !containsAny(string(raw), "mcp-secret") {
		t.Errorf("MCP secrets lost after SaveSecrets:\n%s", string(raw))
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(sub) > 0 && contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
