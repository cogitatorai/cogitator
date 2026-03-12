package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// SecretsData holds sensitive values stored separately from the main config.
type SecretsData struct {
	Providers map[string]ProviderSecret `yaml:"providers"`
	Channels  ChannelSecrets            `yaml:"channels"`
	GitHub    GitHubSecret              `yaml:"github"`
}

// ProviderSecret holds the API key for a single provider.
type ProviderSecret struct {
	APIKey string `yaml:"api_key"`
}

// ChannelSecrets groups channel-level secrets.
type ChannelSecrets struct {
	Telegram TelegramSecret `yaml:"telegram"`
}

// TelegramSecret holds the Telegram bot token.
type TelegramSecret struct {
	BotToken string `yaml:"bot_token"`
}

// GitHubSecret holds a personal access token for GitHub API access (e.g. private repo updates).
type GitHubSecret struct {
	Token string `yaml:"token"`
}

// clearSecrets zeroes all secret fields in cfg so they are omitted
// during YAML marshaling (the fields use omitempty).
func clearSecrets(cfg *Config) {
	for name, pc := range cfg.Providers {
		pc.APIKey = ""
		cfg.Providers[name] = pc
	}
	cfg.Channels.Telegram.BotToken = ""
}

// ExtractSecrets pulls secrets out of cfg into a SecretsData value.
func ExtractSecrets(cfg *Config) SecretsData {
	s := SecretsData{
		Providers: make(map[string]ProviderSecret, len(cfg.Providers)),
	}
	for name, pc := range cfg.Providers {
		if pc.APIKey != "" {
			s.Providers[name] = ProviderSecret{APIKey: pc.APIKey}
		}
	}
	if cfg.Channels.Telegram.BotToken != "" {
		s.Channels.Telegram.BotToken = cfg.Channels.Telegram.BotToken
	}
	return s
}

// ApplySecrets merges secrets back into cfg.
func ApplySecrets(cfg *Config, s SecretsData) {
	for name, ps := range s.Providers {
		if ps.APIKey != "" {
			cfg.SetProviderAPIKey(name, ps.APIKey)
		}
	}
	if s.Channels.Telegram.BotToken != "" {
		cfg.Channels.Telegram.BotToken = s.Channels.Telegram.BotToken
	}
}

// LoadSecretsFromStore reads app-level secrets from the SecretStore.
func LoadSecretsFromStore(store secretstore.SecretStore) (SecretsData, error) {
	var s SecretsData
	s.Providers = make(map[string]ProviderSecret)

	keys, _ := store.List("app")
	for _, key := range keys {
		if strings.HasPrefix(key, "provider:") {
			name := strings.TrimPrefix(key, "provider:")
			val, err := store.Get("app", key)
			if err == nil {
				s.Providers[name] = ProviderSecret{APIKey: val}
			}
		}
	}

	if val, err := store.Get("app", "github_token"); err == nil {
		s.GitHub.Token = val
	}
	if val, err := store.Get("app", "telegram_bot_token"); err == nil {
		s.Channels.Telegram.BotToken = val
	}

	return s, nil
}

// SaveSecretsToStore writes app-level secrets to the SecretStore.
func SaveSecretsToStore(store secretstore.SecretStore, s SecretsData) error {
	for name, p := range s.Providers {
		if p.APIKey != "" {
			if err := store.Set("app", "provider:"+name, p.APIKey); err != nil {
				return err
			}
		}
	}
	if s.GitHub.Token != "" {
		if err := store.Set("app", "github_token", s.GitHub.Token); err != nil {
			return err
		}
	}
	if s.Channels.Telegram.BotToken != "" {
		if err := store.Set("app", "telegram_bot_token", s.Channels.Telegram.BotToken); err != nil {
			return err
		}
	}
	return nil
}

// LoadSecrets reads and unmarshals the secrets file at path.
// Returns an empty SecretsData (not an error) if the file does not exist.
func LoadSecrets(path string) (SecretsData, error) {
	var s SecretsData
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return s, err
	}
	return s, nil
}

// SaveSecrets merges the managed secret fields (providers, channels) into
// the existing secrets file at path, preserving any keys it does not manage
// (e.g. github, mcp). The file is written with 0600 permissions.
func SaveSecrets(path string, s SecretsData) error {
	var root map[string]any

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		root = make(map[string]any)
	} else {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return err
		}
		if root == nil {
			root = make(map[string]any)
		}
	}

	// Update only the keys that ExtractSecrets manages.
	if len(s.Providers) > 0 {
		root["providers"] = s.Providers
	} else {
		delete(root, "providers")
	}
	if s.Channels.Telegram.BotToken != "" {
		root["channels"] = s.Channels
	}
	// Note: github and mcp keys are intentionally left untouched.

	out, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
