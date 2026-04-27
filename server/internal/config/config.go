package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// OptimizationConfig holds knobs for LLM cost-reduction strategies.
// All fields default to false/zero (opt-in only).
type OptimizationConfig struct {
	// ToolsOffFastPath skips sending the tool schema on the first LLM call when
	// the incoming message looks like a conversational ack or greeting. If the
	// model returns empty content, the call is retried once with tools included.
	ToolsOffFastPath bool `yaml:"tools_off_fast_path" json:"tools_off_fast_path"`
}

type Config struct {
	Server       ServerConfig              `yaml:"server"`
	Models       ModelsConfig              `yaml:"models"`
	Providers    map[string]ProviderConfig `yaml:"providers"`
	Resources    ResourcesConfig           `yaml:"resources"`
	Memory       MemoryConfig              `yaml:"memory"`
	Reflection   ReflectionConfig          `yaml:"reflection"`
	Tasks        TasksConfig               `yaml:"tasks"`
	Workspace    WorkspaceConfig           `yaml:"workspace"`
	Channels     ChannelsConfig            `yaml:"channels"`
	Security     SecurityConfig            `yaml:"security"`
	Update       UpdateConfig              `yaml:"update"`
	Voice        VoiceConfig               `yaml:"voice"`
	Optimization OptimizationConfig        `yaml:"optimization"`
}

type UpdateConfig struct {
	SkippedVersion string `yaml:"skipped_version,omitempty"`
}

type VoiceConfig struct {
	STTProvider    string `json:"stt_provider" yaml:"stt_provider"`
	TTSProvider    string `json:"tts_provider" yaml:"tts_provider"`
	TTSVoice       string `json:"tts_voice" yaml:"tts_voice"`
	AudioFormat    string `json:"audio_format" yaml:"audio_format"`
	MaxUploadBytes int    `json:"max_upload_bytes" yaml:"max_upload_bytes"`
	STTTimeoutSec  int    `json:"stt_timeout_s" yaml:"stt_timeout_s"`
}

type SecurityConfig struct {
	SensitivePaths    []string `yaml:"sensitive_paths"`
	DangerousCommands []string `yaml:"dangerous_commands"`
	AllowedDomains    []string `yaml:"allowed_domains"`
	Sandbox           string   `yaml:"sandbox"`
	MaxOutputBytes    int      `yaml:"max_output_bytes"`
}

type ServerConfig struct {
	Port      int    `yaml:"port"`
	Host      string `yaml:"host"`
	PublicURL string `yaml:"public_url,omitempty"`
}

type ModelEntry struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type ModelsConfig struct {
	Standard ModelEntry `yaml:"standard"`
	Cheap    ModelEntry `yaml:"cheap"`
}

type ProviderConfig struct {
	APIKey string `yaml:"api_key,omitempty"`
}

// ProviderAPIKey looks up the API key for the given provider name.
func (c *Config) ProviderAPIKey(name string) string {
	if c.Providers == nil {
		return ""
	}
	return c.Providers[name].APIKey
}

// SetProviderAPIKey sets the API key for the given provider name,
// initializing the map if needed.
func (c *Config) SetProviderAPIKey(name, key string) {
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	c.Providers[name] = ProviderConfig{APIKey: key}
}

type ResourcesConfig struct {
	Mode               string `yaml:"mode"`
	MaxConcurrentTasks int    `yaml:"max_concurrent_tasks"`
	// Budget and rate limit fields are reserved for the upcoming plans system.
	// They are excluded from YAML serialization so stale config values are ignored.
	CheapModelRPM       int `yaml:"-"`
	StandardModelRPM    int `yaml:"-"`
	DailyBudgetCheap    int `yaml:"-"`
	DailyBudgetStandard int `yaml:"-"`
}

type MemoryConfig struct {
	RetrievalTopK    int    `yaml:"retrieval_top_k"`
	MaxRetrievalHops int    `yaml:"max_retrieval_hops"`
	EmbeddingModel   string `yaml:"embedding_model"`
	ContextWindow       int     `yaml:"context_window"`
	ProfileRegenThresh  int     `yaml:"profile_regen_threshold"`
	ConsolidationMin      int     `yaml:"consolidation_min"`
	ConsolidationMax      int     `yaml:"consolidation_max"`
	ConsolidationScale    int     `yaml:"consolidation_scale"`
	RetrievalTokenBudget  int     `yaml:"retrieval_token_budget"`
	RetrievalMinSimilarity float64 `yaml:"retrieval_min_similarity"`
	RetrievalTypeBoost    float64 `yaml:"retrieval_type_boost"`
	EnrichmentVersion        int     `yaml:"enrichment_version"`
	DedupSimilarityThreshold float64 `yaml:"dedup_similarity_threshold"`
}

type ReflectionConfig struct {
	MessageInterval     int    `yaml:"message_interval"`
	IdleTimeoutMinutes  int    `yaml:"idle_timeout_minutes"`
	ProfileRevisionCron string `yaml:"profile_revision_cron"`
}

type TasksConfig struct {
	MaxConcurrent  int `yaml:"max_concurrent"`
	DefaultTimeout int `yaml:"default_timeout"`
	MaxRetries     int `yaml:"max_retries"`
}

type WorkspaceConfig struct {
	Path string `yaml:"path"`
}

type ChannelsConfig struct {
	Web      WebChannelConfig      `yaml:"web"`
	Telegram TelegramChannelConfig `yaml:"telegram"`
	WhatsApp WhatsAppChannelConfig `yaml:"whatsapp"`
}

type WebChannelConfig struct {
	Enabled bool `yaml:"enabled"`
}

type TelegramChannelConfig struct {
	Enabled        bool    `yaml:"enabled"`
	BotToken       string  `yaml:"bot_token,omitempty"`
	AllowedChatIDs []int64 `yaml:"allowed_chat_ids"`
}

type WhatsAppChannelConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Provider string `yaml:"provider"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 8484,
			Host: "0.0.0.0",
		},
		Resources: ResourcesConfig{
			Mode:               "local",
			MaxConcurrentTasks: 2,
		},
		Memory: MemoryConfig{
			RetrievalTopK:    20,
			MaxRetrievalHops: 1,
			EmbeddingModel:   "text-embedding-3-small",
			ContextWindow:       5,
			ProfileRegenThresh:  5,
			ConsolidationMin:       5,
			ConsolidationMax:       50,
			ConsolidationScale:     20,
			RetrievalTokenBudget:   2000,
			RetrievalMinSimilarity: 0.3,
			RetrievalTypeBoost:     1.1,
			EnrichmentVersion:        1,
			DedupSimilarityThreshold: 0.90,
		},
		Reflection: ReflectionConfig{
			MessageInterval:     5,
			IdleTimeoutMinutes:  60,
			ProfileRevisionCron: "0 3 * * *",
		},
		Tasks: TasksConfig{
			MaxConcurrent:  2,
			DefaultTimeout: 300,
			MaxRetries:     3,
		},
		Workspace: WorkspaceConfig{
			Path: "./data",
		},
		Channels: ChannelsConfig{
			Web: WebChannelConfig{Enabled: true},
		},
		Voice: VoiceConfig{
			TTSVoice:       "alloy",
			AudioFormat:    "mp3",
			MaxUploadBytes: 10 * 1024 * 1024,
			STTTimeoutSec:  30,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ResolveDefaults()
	return cfg, nil
}

// providerEnvKeys maps provider names to the standard environment variable
// names that hold their API keys (e.g. OPENAI_API_KEY for "openai").
var providerEnvKeys = map[string]string{
	"openai":     "OPENAI_API_KEY",
	"anthropic":  "ANTHROPIC_API_KEY",
	"groq":       "GROQ_API_KEY",
	"together":   "TOGETHER_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// LoadDotEnv loads variables from a .env file if it exists.
// It does not override variables already set in the shell environment.
func LoadDotEnv() {
	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("warning: failed to load .env file: %v", err)
		}
	}
}

func (c *Config) ApplyEnv() {
	if v := os.Getenv("COGITATOR_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Server.Port = port
		}
	}
	if v := os.Getenv("COGITATOR_SERVER_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("COGITATOR_RESOURCES_MODE"); v != "" {
		c.Resources.Mode = v
	}
	if v := os.Getenv("COGITATOR_WORKSPACE_PATH"); v != "" {
		c.Workspace.Path = v
	}

	// Model configuration from environment.
	if v := os.Getenv("COGITATOR_MODEL_PROVIDER"); v != "" {
		c.Models.Standard.Provider = v
	}
	if v := os.Getenv("COGITATOR_MODEL"); v != "" {
		c.Models.Standard.Model = v
	}

	// Telegram bot token from environment.
	if v := os.Getenv("COGITATOR_TELEGRAM_BOT_TOKEN"); v != "" {
		c.Channels.Telegram.BotToken = v
		c.Channels.Telegram.Enabled = true
	}

	// Sandbox mode from environment.
	if v := os.Getenv("COGITATOR_SECURITY_SANDBOX"); v != "" {
		c.Security.Sandbox = v
	}
	if v := os.Getenv("COGITATOR_SECURITY_MAX_OUTPUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Security.MaxOutputBytes = n
		}
	}

	// Provider API keys: check COGITATOR_<PROVIDER>_API_KEY first,
	// then fall back to standard env vars (OPENAI_API_KEY, etc.).
	// Only set if no key is already configured (file config takes precedence
	// when a key is explicitly set there).
	for name, stdEnv := range providerEnvKeys {
		if c.ProviderAPIKey(name) != "" {
			continue
		}
		key := os.Getenv("COGITATOR_" + strings.ToUpper(name) + "_API_KEY")
		if key == "" {
			key = os.Getenv(stdEnv)
		}
		if key != "" {
			c.SetProviderAPIKey(name, key)
		}
	}

	// Embedding model from environment.
	if v := os.Getenv("COGITATOR_MEMORY_EMBEDDING_MODEL"); v != "" {
		c.Memory.EmbeddingModel = v
	}
	if v := os.Getenv("COGITATOR_RETRIEVAL_TOKEN_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Memory.RetrievalTokenBudget = n
		}
	}
	if v := os.Getenv("COGITATOR_RETRIEVAL_MIN_SIMILARITY"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Memory.RetrievalMinSimilarity = f
		}
	}
	if v := os.Getenv("COGITATOR_RETRIEVAL_TYPE_BOOST"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Memory.RetrievalTypeBoost = f
		}
	}
	if v := os.Getenv("COGITATOR_ENRICHMENT_VERSION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Memory.EnrichmentVersion = n
		}
	}
	if v := os.Getenv("COGITATOR_DEDUP_SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Memory.DedupSimilarityThreshold = f
		}
	}

	// Optimization toggles.
	if v := os.Getenv("COGITATOR_TOOLS_OFF_FAST_PATH"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			c.Optimization.ToolsOffFastPath = true
		}
	}
}

// ResolveDefaults adjusts config values that depend on other config values.
// Call after Load, after ApplyEnv, and after settings updates.
func (c *Config) ResolveDefaults() {
	if c.Memory.EmbeddingModel == "text-embedding-3-small" &&
		strings.EqualFold(c.Models.Standard.Provider, "ollama") {
		c.Memory.EmbeddingModel = "nomic-embed-text"
	}
}
