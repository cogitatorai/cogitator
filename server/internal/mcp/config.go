package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Config holds the MCP server configuration, mirroring the mcp.json format.
type Config struct {
	Servers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig describes a single MCP server, either local (stdio) or remote
// (SSE / Streamable HTTP). Exactly one of Command or URL must be set.
type ServerConfig struct {
	// Local (stdio) transport fields.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// Remote transport fields.
	URL       string            `json:"url,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`

	// Agent-facing description of what this server provides.
	Instructions string `json:"instructions,omitempty"`
}

// IsRemote reports whether the server uses a remote transport.
func (c ServerConfig) IsRemote() bool { return c.URL != "" }

// LoadConfig reads the MCP config from path. If the file does not exist, an
// empty config with an initialized Servers map is returned. Any other I/O or
// parse error is propagated to the caller.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{Servers: map[string]ServerConfig{}}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]ServerConfig{}
	}
	for name, srv := range cfg.Servers {
		if srv.Command != "" && srv.URL != "" {
			return nil, fmt.Errorf("server %q: command and url are mutually exclusive", name)
		}
		if srv.Command == "" && srv.URL == "" {
			return nil, fmt.Errorf("server %q: one of command or url is required", name)
		}
		if srv.URL != "" {
			if srv.Transport == "" {
				srv.Transport = "streamable-http"
				cfg.Servers[name] = srv
			} else if srv.Transport != "streamable-http" && srv.Transport != "sse" {
				return nil, fmt.Errorf("server %q: unsupported transport %q (want \"streamable-http\" or \"sse\")", name, srv.Transport)
			}
		}
	}
	return &cfg, nil
}

// SaveConfig persists cfg to path as indented JSON followed by a newline.
// The file is created or truncated with mode 0644.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
