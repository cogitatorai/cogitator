package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
)

type mockMCPManager struct {
	callResult  string
	callErr     error
	lastServer  string
	lastTool    string
	serverNames []string
	startErr    error
	started     []string
}

func (m *mockMCPManager) CallTool(ctx context.Context, serverName, toolName string, args json.RawMessage) (string, error) {
	m.lastServer = serverName
	m.lastTool = toolName
	if m.callErr != nil {
		return "", m.callErr
	}
	return m.callResult, nil
}

func (m *mockMCPManager) StartServer(ctx context.Context, name string) error {
	m.started = append(m.started, name)
	return m.startErr
}

func (m *mockMCPManager) ServerNames() []string {
	return m.serverNames
}

func (m *mockMCPManager) ServerInstructions() map[string]string {
	out := make(map[string]string, len(m.serverNames))
	for _, n := range m.serverNames {
		out[n] = ""
	}
	return out
}

func TestExecute_MCPTool(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	reg.Register(ToolDef{
		Name:        "mcp__github__create_issue",
		MCPServer:   "github",
		MCPToolName: "create_issue",
	})
	mock := &mockMCPManager{callResult: "Issue #42 created"}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	result, err := exec.Execute(context.Background(), "mcp__github__create_issue", `{"title":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Issue #42 created" {
		t.Errorf("result = %q, want %q", result, "Issue #42 created")
	}
	if mock.lastServer != "github" {
		t.Errorf("server = %q, want %q", mock.lastServer, "github")
	}
	if mock.lastTool != "create_issue" {
		t.Errorf("tool = %q, want %q", mock.lastTool, "create_issue")
	}
}

func TestExecute_MCPTool_Error(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	reg.Register(ToolDef{
		Name:        "mcp__github__create_issue",
		MCPServer:   "github",
		MCPToolName: "create_issue",
	})
	mock := &mockMCPManager{callErr: fmt.Errorf("server crashed")}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	_, err := exec.Execute(context.Background(), "mcp__github__create_issue", `{"title":"test"}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_MCPTool_SanitizedName(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	// Server name "my.server" gets sanitized to "my_server" in the qualified name,
	// but the registry stores the original name for CallTool routing.
	reg.Register(ToolDef{
		Name:        "mcp__my_server__read_file",
		MCPServer:   "my.server",
		MCPToolName: "read_file",
	})
	mock := &mockMCPManager{callResult: "file contents"}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	result, err := exec.Execute(context.Background(), "mcp__my_server__read_file", `{"path":"a.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "file contents" {
		t.Errorf("result = %q, want %q", result, "file contents")
	}
	if mock.lastServer != "my.server" {
		t.Errorf("server = %q, want original %q", mock.lastServer, "my.server")
	}
	if mock.lastTool != "read_file" {
		t.Errorf("tool = %q, want %q", mock.lastTool, "read_file")
	}
}

func TestExecute_NonMCPTool_NotAffected(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	mock := &mockMCPManager{callResult: "should not be called"}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	// A non-MCP, non-existent tool should still return "unknown tool".
	_, err := exec.Execute(context.Background(), "nonexistent_tool", `{}`)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestExecute_StartMCPServer(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	mock := &mockMCPManager{serverNames: []string{"French_Gov", "github"}}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	result, err := exec.Execute(context.Background(), "start_mcp_server", `{"server_name":"French_Gov"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.started) != 1 || mock.started[0] != "French_Gov" {
		t.Errorf("started = %v, want [French_Gov]", mock.started)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestExecute_StartMCPServer_Unknown(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	mock := &mockMCPManager{serverNames: []string{"github"}}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	_, err := exec.Execute(context.Background(), "start_mcp_server", `{"server_name":"nonexistent"}`)
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestExecute_UnregisteredMCPTool_LazyStart(t *testing.T) {
	reg := NewRegistry(t.TempDir(), slog.Default())
	// Tool is NOT registered in the registry, but follows mcp__ pattern.
	mock := &mockMCPManager{callResult: "lazy result"}
	exec := NewExecutor(reg, t.TempDir(), nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil)
	exec.SetMCPManager(mock)

	result, err := exec.Execute(context.Background(), "mcp__French_Gov__search_datasets", `{"q":"population"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "lazy result" {
		t.Errorf("result = %q, want %q", result, "lazy result")
	}
	if mock.lastServer != "French_Gov" {
		t.Errorf("server = %q, want %q", mock.lastServer, "French_Gov")
	}
	if mock.lastTool != "search_datasets" {
		t.Errorf("tool = %q, want %q", mock.lastTool, "search_datasets")
	}
}
