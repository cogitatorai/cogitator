package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_NonExistent(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "mcp.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Servers == nil {
		t.Fatal("expected initialized Servers map")
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected empty Servers map, got %d entries", len(cfg.Servers))
	}
}

func TestLoadConfig_ValidJSON(t *testing.T) {
	raw := `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": {
        "NODE_ENV": "production"
      }
    }
  }
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv, ok := cfg.Servers["filesystem"]
	if !ok {
		t.Fatal("expected 'filesystem' server entry")
	}
	if srv.Command != "npx" {
		t.Errorf("expected command 'npx', got %q", srv.Command)
	}
	if len(srv.Args) != 3 {
		t.Errorf("expected 3 args, got %d", len(srv.Args))
	}
	if srv.Env["NODE_ENV"] != "production" {
		t.Errorf("expected env NODE_ENV=production, got %q", srv.Env["NODE_ENV"])
	}
}

func TestSaveAndLoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	original := &Config{
		Servers: map[string]ServerConfig{
			"myserver": {
				Command: "/usr/local/bin/myserver",
				Args:    []string{"--port", "8080"},
				Env:     map[string]string{"FOO": "bar"},
			},
			"minimal": {
				Command: "echo",
			},
		},
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after save failed: %v", err)
	}

	if len(loaded.Servers) != len(original.Servers) {
		t.Fatalf("expected %d servers, got %d", len(original.Servers), len(loaded.Servers))
	}

	s := loaded.Servers["myserver"]
	if s.Command != "/usr/local/bin/myserver" {
		t.Errorf("myserver command mismatch: %q", s.Command)
	}
	if len(s.Args) != 2 || s.Args[0] != "--port" || s.Args[1] != "8080" {
		t.Errorf("myserver args mismatch: %v", s.Args)
	}
	if s.Env["FOO"] != "bar" {
		t.Errorf("myserver env mismatch: %v", s.Env)
	}

	m := loaded.Servers["minimal"]
	if m.Command != "echo" {
		t.Errorf("minimal command mismatch: %q", m.Command)
	}
	if len(m.Args) != 0 {
		t.Errorf("minimal args should be empty, got: %v", m.Args)
	}

	// Verify file ends with newline (our convention).
	data, _ := os.ReadFile(path)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("saved file should end with a newline")
	}
}

func TestLoadConfig_RemoteServer(t *testing.T) {
	raw := `{
  "mcpServers": {
    "local": {
      "command": "npx",
      "args": ["-y", "mcp-server"]
    },
    "remote-sse": {
      "url": "https://example.com/sse",
      "transport": "sse",
      "headers": {"Authorization": "Bearer tok"}
    },
    "remote-default": {
      "url": "https://example.com/mcp"
    }
  }
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Local server.
	local := cfg.Servers["local"]
	if local.IsRemote() {
		t.Error("local server should not be remote")
	}
	if local.Command != "npx" {
		t.Errorf("expected command 'npx', got %q", local.Command)
	}

	// Remote SSE server.
	sse := cfg.Servers["remote-sse"]
	if !sse.IsRemote() {
		t.Error("remote-sse should be remote")
	}
	if sse.URL != "https://example.com/sse" {
		t.Errorf("unexpected url: %q", sse.URL)
	}
	if sse.Transport != "sse" {
		t.Errorf("expected transport 'sse', got %q", sse.Transport)
	}
	if sse.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("unexpected Authorization header: %q", sse.Headers["Authorization"])
	}

	// Remote server with default transport.
	def := cfg.Servers["remote-default"]
	if !def.IsRemote() {
		t.Error("remote-default should be remote")
	}
	if def.Transport != "streamable-http" {
		t.Errorf("expected default transport 'streamable-http', got %q", def.Transport)
	}
}

func TestLoadConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string // substring expected in error; empty means no error
	}{
		{
			name:    "both command and url",
			json:    `{"mcpServers":{"s":{"command":"echo","url":"https://x.com"}}}`,
			wantErr: "mutually exclusive",
		},
		{
			name:    "neither command nor url",
			json:    `{"mcpServers":{"s":{}}}`,
			wantErr: "one of command or url is required",
		},
		{
			name:    "invalid transport",
			json:    `{"mcpServers":{"s":{"url":"https://x.com","transport":"grpc"}}}`,
			wantErr: "unsupported transport",
		},
		{
			name:    "url without transport defaults to streamable-http",
			json:    `{"mcpServers":{"s":{"url":"https://x.com"}}}`,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "mcp.json")
			if err := os.WriteFile(path, []byte(tt.json), 0644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Verify default transport was applied.
			if srv, ok := cfg.Servers["s"]; ok && srv.URL != "" {
				if srv.Transport != "streamable-http" {
					t.Errorf("expected default transport 'streamable-http', got %q", srv.Transport)
				}
			}
		})
	}
}

func TestServerConfig_InstructionsRoundTrip(t *testing.T) {
	cfg := &Config{
		Servers: map[string]ServerConfig{
			"test": {
				Command:      "npx",
				Args:         []string{"-y", "server"},
				Instructions: "Test server for unit testing.",
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Servers["test"].Instructions != "Test server for unit testing." {
		t.Errorf("instructions = %q", loaded.Servers["test"].Instructions)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
