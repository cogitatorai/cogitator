package mcp

import (
	"encoding/json"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// ServerSecrets holds sensitive credentials for a remote MCP server.
type ServerSecrets struct {
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	OAuth   *OAuthSecrets     `yaml:"oauth,omitempty" json:"oauth,omitempty"`
}

// OAuthSecrets holds OAuth 2.0 credentials for a remote MCP server.
type OAuthSecrets struct {
	ClientID     string   `yaml:"client_id" json:"client_id"`
	ClientSecret string   `yaml:"client_secret" json:"client_secret"`
	Scopes       []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	RedirectURI  string   `yaml:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
}

// LoadMCPSecrets reads MCP server credentials from the store under the "mcp"
// namespace. Returns an empty map (not nil) if no keys exist.
func LoadMCPSecrets(store secretstore.SecretStore) (map[string]*ServerSecrets, error) {
	keys, err := store.List("mcp")
	if err != nil {
		return make(map[string]*ServerSecrets), nil
	}
	secrets := make(map[string]*ServerSecrets, len(keys))
	for _, key := range keys {
		val, err := store.Get("mcp", key)
		if err != nil {
			continue
		}
		var s ServerSecrets
		if err := json.Unmarshal([]byte(val), &s); err != nil {
			continue
		}
		secrets[key] = &s
	}
	return secrets, nil
}

// SaveMCPSecrets persists MCP server credentials to the store under the "mcp"
// namespace, removing any entries that are no longer present in secrets.
func SaveMCPSecrets(store secretstore.SecretStore, secrets map[string]*ServerSecrets) error {
	existing, _ := store.List("mcp")
	existingSet := make(map[string]bool, len(existing))
	for _, k := range existing {
		existingSet[k] = true
	}
	for name, s := range secrets {
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if err := store.Set("mcp", name, string(data)); err != nil {
			return err
		}
		delete(existingSet, name)
	}
	for name := range existingSet {
		if err := store.Delete("mcp", name); err != nil {
			return err
		}
	}
	return nil
}
