package connector

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Manifest is the top-level connector definition parsed from connector.yaml.
type Manifest struct {
	Name        string         `yaml:"name"`
	DisplayName string         `yaml:"display_name"`
	Description string         `yaml:"description"`
	Version     string         `yaml:"version"`
	Auth        AuthConfig     `yaml:"auth"`
	Tools       []ToolManifest `yaml:"tools"`
	Embedded    bool           `yaml:"-"`
}

// AuthConfig defines OAuth2 parameters for the connector.
type AuthConfig struct {
	Type     string   `yaml:"type"`
	AuthURL  string   `yaml:"auth_url"`
	TokenURL string   `yaml:"token_url"`
	Scopes   []string `yaml:"scopes"`
}

// ToolManifest defines a single tool exposed by the connector.
type ToolManifest struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Parameters  []ParamDef    `yaml:"parameters"`
	Request     RequestDef    `yaml:"request"`
	Response    ResponseDef   `yaml:"response"`
	FetchEach   *FetchEachDef `yaml:"fetch_each,omitempty"`

	connectorName string // set after parsing
}

// ParamDef defines a tool parameter.
type ParamDef struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
}

// RequestDef defines the HTTP request template for a tool.
type RequestDef struct {
	Method string            `yaml:"method"`
	URL    string            `yaml:"url"`
	Query  map[string]string `yaml:"query,omitempty"`
	Body   string            `yaml:"body,omitempty"`
}

// ResponseDef defines how to extract fields from the API response.
type ResponseDef struct {
	Root   string            `yaml:"root,omitempty"`
	Fields map[string]string `yaml:"fields,omitempty"`
}

// FetchEachDef defines a list+fetch pattern (e.g., Gmail search then fetch each message).
type FetchEachDef struct {
	IDPath   string      `yaml:"id_path"`
	Request  RequestDef  `yaml:"request"`
	Response ResponseDef `yaml:"response"`
}

// QualifiedName returns the namespaced tool name: {connector}_{tool}.
func (t *ToolManifest) QualifiedName() string {
	return t.connectorName + "_" + t.Name
}

// ProviderSchema converts the parameter list to the JSON Schema format
// expected by the LLM provider (OpenAI function calling format).
func (t *ToolManifest) ProviderSchema() map[string]any {
	props := make(map[string]any, len(t.Parameters))
	var required []string
	for _, p := range t.Parameters {
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		props[p.Name] = map[string]any{
			"type":        typ,
			"description": p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// ParseManifest reads and validates a connector.yaml file.
func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}
	// Set connector name on each tool.
	for i := range m.Tools {
		m.Tools[i].connectorName = m.Name
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if m.Auth.Type != "" && m.Auth.Type != "oauth2" {
		return fmt.Errorf("unsupported auth type: %s", m.Auth.Type)
	}
	if m.Auth.Type == "oauth2" {
		if m.Auth.AuthURL == "" {
			return fmt.Errorf("auth.auth_url is required for oauth2")
		}
		if m.Auth.TokenURL == "" {
			return fmt.Errorf("auth.token_url is required for oauth2")
		}
	}
	for i, t := range m.Tools {
		if t.Name == "" {
			return fmt.Errorf("tool[%d]: name is required", i)
		}
		if t.Request.URL == "" {
			return fmt.Errorf("tool %q: request.url is required", t.Name)
		}
	}
	return nil
}
