package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 8484 {
		t.Errorf("expected default port 8484, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host '0.0.0.0', got %s", cfg.Server.Host)
	}
	if cfg.Resources.Mode != "local" {
		t.Errorf("expected default resource mode 'local', got %s", cfg.Resources.Mode)
	}
	if cfg.Resources.MaxConcurrentTasks != 2 {
		t.Errorf("expected max concurrent tasks 2, got %d", cfg.Resources.MaxConcurrentTasks)
	}
	if cfg.Memory.RetrievalTopK != 20 {
		t.Errorf("expected default retrieval top-k 20, got %d", cfg.Memory.RetrievalTopK)
	}
	if cfg.Memory.MaxRetrievalHops != 1 {
		t.Errorf("expected default max retrieval hops 1, got %d", cfg.Memory.MaxRetrievalHops)
	}
	if cfg.Reflection.MessageInterval != 5 {
		t.Errorf("expected default message interval 5, got %d", cfg.Reflection.MessageInterval)
	}
	if cfg.Reflection.ProfileRevisionCron != "0 3 * * *" {
		t.Errorf("expected default cron '0 3 * * *', got %s", cfg.Reflection.ProfileRevisionCron)
	}
	if cfg.Tasks.MaxConcurrent != 2 {
		t.Errorf("expected default max concurrent 2, got %d", cfg.Tasks.MaxConcurrent)
	}
	if cfg.Workspace.Path != "./data" {
		t.Errorf("expected default workspace './data', got %s", cfg.Workspace.Path)
	}
	if !cfg.Channels.Web.Enabled {
		t.Error("expected web channel enabled by default")
	}
	if cfg.Channels.Telegram.Enabled {
		t.Error("expected telegram channel disabled by default")
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cogitator.yaml")
	os.WriteFile(cfgPath, []byte(`
server:
  port: 9090
  host: "127.0.0.1"
resources:
  mode: managed
  daily_budget_cheap: 100000
models:
  standard:
    provider: anthropic
    model: claude-sonnet-4-20250514
  cheap:
    provider: anthropic
    model: claude-haiku-4-20250414
providers:
  anthropic:
    api_key: "sk-ant-test"
channels:
  telegram:
    enabled: true
    bot_token: "test-token"
`), 0o644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host '127.0.0.1', got %s", cfg.Server.Host)
	}
	if cfg.Resources.Mode != "managed" {
		t.Errorf("expected mode 'managed', got %s", cfg.Resources.Mode)
	}
	// Budget fields use yaml:"-" and are no longer loaded from config files.
	if cfg.Resources.DailyBudgetCheap != 0 {
		t.Errorf("expected budget 0 (yaml ignored), got %d", cfg.Resources.DailyBudgetCheap)
	}
	if cfg.Models.Standard.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %s", cfg.Models.Standard.Provider)
	}
	if cfg.ProviderAPIKey("anthropic") != "sk-ant-test" {
		t.Errorf("expected anthropic api key 'sk-ant-test', got %q", cfg.ProviderAPIKey("anthropic"))
	}
	if !cfg.Channels.Telegram.Enabled {
		t.Error("expected telegram enabled")
	}
	if cfg.Channels.Telegram.BotToken != "test-token" {
		t.Errorf("expected bot token 'test-token', got %s", cfg.Channels.Telegram.BotToken)
	}
	// Unset values should keep defaults
	if cfg.Memory.RetrievalTopK != 20 {
		t.Errorf("expected default retrieval top-k 20, got %d", cfg.Memory.RetrievalTopK)
	}
	if cfg.Tasks.MaxConcurrent != 2 {
		t.Errorf("expected default max concurrent 2, got %d", cfg.Tasks.MaxConcurrent)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/cogitator.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("COGITATOR_SERVER_PORT", "7777")
	t.Setenv("COGITATOR_SERVER_HOST", "localhost")
	t.Setenv("COGITATOR_RESOURCES_MODE", "managed")
	t.Setenv("COGITATOR_WORKSPACE_PATH", "/custom/path")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Server.Port != 7777 {
		t.Errorf("expected port 7777 from env, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "localhost" {
		t.Errorf("expected host 'localhost' from env, got %s", cfg.Server.Host)
	}
	if cfg.Resources.Mode != "managed" {
		t.Errorf("expected mode 'managed' from env, got %s", cfg.Resources.Mode)
	}
	if cfg.Workspace.Path != "/custom/path" {
		t.Errorf("expected workspace '/custom/path' from env, got %s", cfg.Workspace.Path)
	}
}

func TestEnvInvalidPort(t *testing.T) {
	t.Setenv("COGITATOR_SERVER_PORT", "not-a-number")
	cfg := Default()
	cfg.ApplyEnv()
	if cfg.Server.Port != 8484 {
		t.Errorf("expected default port preserved on invalid env, got %d", cfg.Server.Port)
	}
}

func TestEnvProviderAPIKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-openai")

	cfg := Default()
	cfg.ApplyEnv()

	if got := cfg.ProviderAPIKey("openai"); got != "sk-test-openai" {
		t.Errorf("expected openai key from OPENAI_API_KEY, got %q", got)
	}
}

func TestEnvProviderCogitatorPrefixTakesPrecedence(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-standard")
	t.Setenv("COGITATOR_OPENAI_API_KEY", "sk-cogitator")

	cfg := Default()
	cfg.ApplyEnv()

	if got := cfg.ProviderAPIKey("openai"); got != "sk-cogitator" {
		t.Errorf("expected cogitator-prefixed key to win, got %q", got)
	}
}

func TestEnvProviderKeySkippedWhenFileConfigured(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	cfg := Default()
	cfg.SetProviderAPIKey("openai", "sk-from-file")
	cfg.ApplyEnv()

	if got := cfg.ProviderAPIKey("openai"); got != "sk-from-file" {
		t.Errorf("expected file key preserved, got %q", got)
	}
}

func TestMergeAllowedDomains(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cogitator.yaml")

	cfg := Default()
	cfg.Security.AllowedDomains = []string{"existing.com"}
	store := NewStore(cfg, cfgPath, nil)

	merged, err := store.MergeAllowedDomains([]string{"new.io", "existing.com", "another.org"})
	if err != nil {
		t.Fatalf("MergeAllowedDomains() error: %v", err)
	}

	want := []string{"existing.com", "new.io", "another.org"}
	if len(merged) != len(want) {
		t.Fatalf("merged = %v, want %v", merged, want)
	}
	for i := range merged {
		if merged[i] != want[i] {
			t.Errorf("merged[%d] = %q, want %q", i, merged[i], want[i])
		}
	}

	// Verify file was written.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading persisted config: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("persisted config is empty")
	}
}

func TestMergeAllowedDomainsNoop(t *testing.T) {
	cfg := Default()
	cfg.Security.AllowedDomains = []string{"a.com"}
	store := NewStore(cfg, "", nil)

	merged, err := store.MergeAllowedDomains([]string{"a.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 1 || merged[0] != "a.com" {
		t.Errorf("expected no change, got %v", merged)
	}
}

func TestEnvModelOverrides(t *testing.T) {
	t.Setenv("COGITATOR_MODEL_PROVIDER", "anthropic")
	t.Setenv("COGITATOR_MODEL", "claude-sonnet-4-20250514")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Models.Standard.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", cfg.Models.Standard.Provider)
	}
	if cfg.Models.Standard.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got %q", cfg.Models.Standard.Model)
	}
}

func TestApplyEnv_EmbeddingModel(t *testing.T) {
	t.Setenv("COGITATOR_MEMORY_EMBEDDING_MODEL", "nomic-embed-text")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Memory.EmbeddingModel != "nomic-embed-text" {
		t.Errorf("expected nomic-embed-text, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestApplyEnv_EmbeddingModel_NotSet(t *testing.T) {
	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Memory.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("expected default text-embedding-3-small, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestResolveDefaults_OllamaProvider(t *testing.T) {
	cfg := Default()
	cfg.Models.Standard.Provider = "ollama"
	cfg.ResolveDefaults()

	if cfg.Memory.EmbeddingModel != "nomic-embed-text" {
		t.Errorf("expected nomic-embed-text for ollama, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestResolveDefaults_OllamaWithExplicitModel(t *testing.T) {
	cfg := Default()
	cfg.Models.Standard.Provider = "ollama"
	cfg.Memory.EmbeddingModel = "mxbai-embed-large"
	cfg.ResolveDefaults()

	if cfg.Memory.EmbeddingModel != "mxbai-embed-large" {
		t.Errorf("expected mxbai-embed-large (explicit), got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestResolveDefaults_OpenAIProvider(t *testing.T) {
	cfg := Default()
	cfg.Models.Standard.Provider = "openai"
	cfg.ResolveDefaults()

	if cfg.Memory.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("expected text-embedding-3-small for openai, got %q", cfg.Memory.EmbeddingModel)
	}
}

func TestMemoryConfigNewFields(t *testing.T) {
	cfg := Default()
	if cfg.Memory.RetrievalTokenBudget != 2000 {
		t.Errorf("RetrievalTokenBudget default = %d, want 2000", cfg.Memory.RetrievalTokenBudget)
	}
	if cfg.Memory.RetrievalMinSimilarity != 0.3 {
		t.Errorf("RetrievalMinSimilarity default = %f, want 0.3", cfg.Memory.RetrievalMinSimilarity)
	}
	if cfg.Memory.RetrievalTypeBoost != 1.1 {
		t.Errorf("RetrievalTypeBoost default = %f, want 1.1", cfg.Memory.RetrievalTypeBoost)
	}
	if cfg.Memory.EnrichmentVersion != 1 {
		t.Errorf("EnrichmentVersion default = %d, want 1", cfg.Memory.EnrichmentVersion)
	}
	if cfg.Memory.RetrievalTopK != 20 {
		t.Errorf("RetrievalTopK default = %d, want 20", cfg.Memory.RetrievalTopK)
	}
}

func TestApplyEnvNewMemoryFields(t *testing.T) {
	t.Setenv("COGITATOR_RETRIEVAL_TOKEN_BUDGET", "3000")
	t.Setenv("COGITATOR_RETRIEVAL_MIN_SIMILARITY", "0.4")
	t.Setenv("COGITATOR_RETRIEVAL_TYPE_BOOST", "1.2")
	t.Setenv("COGITATOR_ENRICHMENT_VERSION", "2")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Memory.RetrievalTokenBudget != 3000 {
		t.Errorf("RetrievalTokenBudget = %d, want 3000", cfg.Memory.RetrievalTokenBudget)
	}
	if cfg.Memory.RetrievalMinSimilarity != 0.4 {
		t.Errorf("RetrievalMinSimilarity = %f, want 0.4", cfg.Memory.RetrievalMinSimilarity)
	}
	if cfg.Memory.RetrievalTypeBoost != 1.2 {
		t.Errorf("RetrievalTypeBoost = %f, want 1.2", cfg.Memory.RetrievalTypeBoost)
	}
	if cfg.Memory.EnrichmentVersion != 2 {
		t.Errorf("EnrichmentVersion = %d, want 2", cfg.Memory.EnrichmentVersion)
	}
}

func TestDedupSimilarityThresholdDefault(t *testing.T) {
	cfg := Default()
	if cfg.Memory.DedupSimilarityThreshold != 0.90 {
		t.Errorf("DedupSimilarityThreshold default = %f, want 0.90", cfg.Memory.DedupSimilarityThreshold)
	}
}

func TestApplyEnvDedupThreshold(t *testing.T) {
	t.Setenv("COGITATOR_DEDUP_SIMILARITY_THRESHOLD", "0.95")
	cfg := Default()
	cfg.ApplyEnv()
	if cfg.Memory.DedupSimilarityThreshold != 0.95 {
		t.Errorf("DedupSimilarityThreshold = %f, want 0.95", cfg.Memory.DedupSimilarityThreshold)
	}
}
