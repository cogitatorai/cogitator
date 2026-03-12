package secretstore

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// legacyTokenInfo represents an OAuth token as stored in connector_tokens.yaml.
type legacyTokenInfo struct {
	AccessToken  string `yaml:"access_token"  json:"access_token"`
	RefreshToken string `yaml:"refresh_token"  json:"refresh_token"`
	TokenType    string `yaml:"token_type"     json:"token_type"`
	Expiry       string `yaml:"expiry"         json:"expiry,omitempty"`
	ClientID     string `yaml:"client_id"      json:"client_id,omitempty"`
	ClientSecret string `yaml:"client_secret"  json:"client_secret,omitempty"`
}

// legacyConnectorTokensFile mirrors the top-level structure of connector_tokens.yaml.
type legacyConnectorTokensFile struct {
	Connectors map[string]map[string]legacyTokenInfo `yaml:"connectors"`
}

// legacySecretsFile mirrors the top-level structure of secrets.yaml.
type legacySecretsFile struct {
	Providers map[string]struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"providers"`
	Channels struct {
		Telegram struct {
			BotToken string `yaml:"bot_token"`
		} `yaml:"telegram"`
	} `yaml:"channels"`
	GitHub struct {
		Token string `yaml:"token"`
	} `yaml:"github"`
	Relay struct {
		Token string `yaml:"token"`
	} `yaml:"relay"`
	MCP map[string]map[string]any `yaml:"mcp"`
}

// MigrateFiles migrates secrets from legacy YAML files into the SecretStore.
// Files successfully migrated are renamed to .bak. If .bak already exists,
// that file is skipped (idempotent). Errors are logged but do not halt migration.
func MigrateFiles(store SecretStore, workspaceRoot string) error {
	if err := migrateConnectorTokens(store, workspaceRoot); err != nil {
		slog.Warn("secretstore: connector token migration failed", "error", err)
	}
	if err := migrateSecrets(store, workspaceRoot); err != nil {
		slog.Warn("secretstore: secrets migration failed", "error", err)
	}
	return nil
}

// migrateConnectorTokens reads connector_tokens.yaml, writes each token into
// the store under namespace "connector", then renames the file to .bak.
func migrateConnectorTokens(store SecretStore, workspaceRoot string) error {
	src := filepath.Join(workspaceRoot, "connector_tokens.yaml")
	bak := src + ".bak"

	if _, err := os.Stat(bak); err == nil {
		return nil // already migrated
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // nothing to migrate
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	var f legacyConnectorTokensFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}

	for connectorName, users := range f.Connectors {
		for userID, token := range users {
			raw, err := json.Marshal(token)
			if err != nil {
				return fmt.Errorf("marshal token %s:%s: %w", connectorName, userID, err)
			}
			key := connectorName + ":" + userID
			if err := store.Set("connector", key, string(raw)); err != nil {
				return fmt.Errorf("store connector %s: %w", key, err)
			}
		}
	}

	if err := os.Rename(src, bak); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", src, bak, err)
	}
	return nil
}

// migrateSecrets reads secrets.yaml, writes each secret into the store, then
// renames the file to .bak.
func migrateSecrets(store SecretStore, workspaceRoot string) error {
	src := filepath.Join(workspaceRoot, "secrets.yaml")
	bak := src + ".bak"

	if _, err := os.Stat(bak); err == nil {
		return nil // already migrated
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil // nothing to migrate
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	var f legacySecretsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}

	if f.GitHub.Token != "" {
		if err := store.Set("app", "github_token", f.GitHub.Token); err != nil {
			return fmt.Errorf("store github_token: %w", err)
		}
	}

	if f.Relay.Token != "" {
		if err := store.Set("app", "relay_token", f.Relay.Token); err != nil {
			return fmt.Errorf("store relay_token: %w", err)
		}
	}

	for name, p := range f.Providers {
		if p.APIKey == "" {
			continue
		}
		if err := store.Set("app", "provider:"+name, p.APIKey); err != nil {
			return fmt.Errorf("store provider:%s: %w", name, err)
		}
	}

	if tok := f.Channels.Telegram.BotToken; tok != "" {
		if err := store.Set("app", "telegram_bot_token", tok); err != nil {
			return fmt.Errorf("store telegram_bot_token: %w", err)
		}
	}

	for serverName, serverSecrets := range f.MCP {
		raw, err := json.Marshal(serverSecrets)
		if err != nil {
			return fmt.Errorf("marshal mcp %s: %w", serverName, err)
		}
		if err := store.Set("mcp", serverName, string(raw)); err != nil {
			return fmt.Errorf("store mcp/%s: %w", serverName, err)
		}
	}

	if err := os.Rename(src, bak); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", src, bak, err)
	}
	return nil
}
