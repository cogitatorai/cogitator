package mcp

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// newTestManager creates a Manager backed by a temp directory.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	store := secretstore.NewFileStore(dir)
	m := NewManager(path, store, nil, nil)
	if err := m.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return m, path
}

func TestNewManager_NoConfigFile(t *testing.T) {
	m, _ := newTestManager(t)
	servers := m.Servers()
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(servers))
	}
}

func TestAddServer_Persists(t *testing.T) {
	m, path := newTestManager(t)

	cfg := ServerConfig{
		Command: "/usr/bin/myserver",
		Args:    []string{"--verbose"},
		Env:     map[string]string{"DEBUG": "1"},
	}
	if err := m.AddServer("myserver", cfg); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	// Verify in-memory state.
	servers := m.Servers()
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Name != "myserver" {
		t.Errorf("expected name 'myserver', got %q", servers[0].Name)
	}
	if servers[0].Status != StatusStopped {
		t.Errorf("expected status stopped, got %q", servers[0].Status)
	}

	// Verify persisted to disk by loading fresh.
	diskCfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig from disk: %v", err)
	}
	sc, ok := diskCfg.Servers["myserver"]
	if !ok {
		t.Fatal("server not found on disk")
	}
	if sc.Command != "/usr/bin/myserver" {
		t.Errorf("disk command mismatch: %q", sc.Command)
	}
}

func TestAddServer_Duplicate(t *testing.T) {
	m, _ := newTestManager(t)

	cfg := ServerConfig{Command: "echo"}
	if err := m.AddServer("dup", cfg); err != nil {
		t.Fatalf("first AddServer: %v", err)
	}
	if err := m.AddServer("dup", cfg); err == nil {
		t.Fatal("expected error on duplicate AddServer, got nil")
	}
}

func TestRemoveServer_Persists(t *testing.T) {
	m, path := newTestManager(t)

	if err := m.AddServer("todelete", ServerConfig{Command: "echo"}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if err := m.RemoveServer("todelete"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}

	if len(m.Servers()) != 0 {
		t.Errorf("expected 0 servers after remove, got %d", len(m.Servers()))
	}

	diskCfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, ok := diskCfg.Servers["todelete"]; ok {
		t.Error("server should have been removed from disk")
	}
}

func TestRemoveServer_NotFound(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.RemoveServer("ghost"); err == nil {
		t.Fatal("expected error removing nonexistent server")
	}
}

func TestServerStatus_InitialStopped(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.AddServer("s1", ServerConfig{Command: "echo"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Servers() {
		if s.Status != StatusStopped {
			t.Errorf("expected stopped, got %q", s.Status)
		}
		if s.StartedAt != nil {
			t.Error("StartedAt should be nil for stopped server")
		}
		if s.ToolCount != 0 {
			t.Errorf("ToolCount should be 0 for stopped server, got %d", s.ToolCount)
		}
	}
}

func TestSanitizeToolNamePart(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"has.dots", "has_dots"},
		{"has spaces", "has_spaces"},
		{"MixedCase123", "MixedCase123"},
		{"a/b:c@d", "a_b_c_d"},
	}
	for _, tc := range cases {
		got := sanitizeToolNamePart(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeToolNamePart(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestQualifiedToolName(t *testing.T) {
	cases := []struct {
		server, tool, want string
	}{
		{"filesystem", "read_file", "mcp__filesystem__read_file"},
		{"my-server", "do_thing", "mcp__my-server__do_thing"},
		{"my.server", "read_file", "mcp__my_server__read_file"},
		{"my server", "get info", "mcp__my_server__get_info"},
	}
	for _, tc := range cases {
		got := QualifiedToolName(tc.server, tc.tool)
		if got != tc.want {
			t.Errorf("QualifiedToolName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
		}
	}
}

func TestParseQualifiedToolName(t *testing.T) {
	ok_cases := []struct {
		input, server, tool string
	}{
		{"mcp__filesystem__read_file", "filesystem", "read_file"},
		{"mcp__my-server__do_thing", "my-server", "do_thing"},
		{"mcp__s__t", "s", "t"},
	}
	for _, tc := range ok_cases {
		server, tool, ok := ParseQualifiedToolName(tc.input)
		if !ok {
			t.Errorf("ParseQualifiedToolName(%q): expected ok=true", tc.input)
			continue
		}
		if server != tc.server || tool != tc.tool {
			t.Errorf("ParseQualifiedToolName(%q) = (%q, %q), want (%q, %q)", tc.input, server, tool, tc.server, tc.tool)
		}
	}

	bad_cases := []string{
		"",
		"filesystem__read_file",
		"mcp__",
		"mcp__nounderscores",
		"mcp____tool",   // empty server
		"mcp__server__", // empty tool
	}
	for _, input := range bad_cases {
		_, _, ok := ParseQualifiedToolName(input)
		if ok {
			t.Errorf("ParseQualifiedToolName(%q): expected ok=false", input)
		}
	}
}

func TestRoundTrip_AddLoadServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	store := secretstore.NewFileStore(dir)

	m1 := NewManager(path, store, nil, nil)
	if err := m1.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if err := m1.AddServer("alpha", ServerConfig{Command: "alpha-bin", Args: []string{"--fast"}}); err != nil {
		t.Fatal(err)
	}
	if err := m1.AddServer("beta", ServerConfig{Command: "beta-bin", Env: map[string]string{"X": "1"}}); err != nil {
		t.Fatal(err)
	}

	// Simulate restart: new manager instance loads from disk.
	m2 := NewManager(path, store, nil, nil)
	if err := m2.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	servers := m2.Servers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers after reload, got %d", len(servers))
	}
}

func TestManager_SecretsLoading(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")

	os.WriteFile(cfgPath, []byte(`{
		"mcpServers": {
			"remote": {
				"url": "https://example.com/mcp",
				"headers": {"X-Base": "from-config"}
			}
		}
	}`), 0644)

	store := secretstore.NewFileStore(dir)
	if err := SaveMCPSecrets(store, map[string]*ServerSecrets{
		"remote": {
			Headers: map[string]string{
				"Authorization": "Bearer secret",
				"X-Base":        "from-secrets",
			},
		},
	}); err != nil {
		t.Fatalf("pre-populate secrets: %v", err)
	}

	m := NewManager(cfgPath, store, nil, slog.Default())
	if err := m.LoadConfig(); err != nil {
		t.Fatalf("load: %v", err)
	}

	secrets := m.ServerSecrets("remote")
	if secrets == nil {
		t.Fatal("expected secrets for remote")
	}
	if secrets.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("auth header = %q", secrets.Headers["Authorization"])
	}

	if m.ServerSecrets("nonexistent") != nil {
		t.Error("expected nil for nonexistent server")
	}
}

func TestMergeHeaders(t *testing.T) {
	base := map[string]string{"X-Base": "base-val", "Keep": "yes"}
	secret := map[string]string{"X-Base": "secret-val", "Auth": "token"}
	merged := mergeHeaders(base, secret)

	if merged["X-Base"] != "secret-val" {
		t.Errorf("expected secret to override, got %q", merged["X-Base"])
	}
	if merged["Keep"] != "yes" {
		t.Errorf("expected base value preserved, got %q", merged["Keep"])
	}
	if merged["Auth"] != "token" {
		t.Errorf("expected secret value added, got %q", merged["Auth"])
	}
}
