package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/sandbox"
	"github.com/cogitatorai/cogitator/server/internal/security"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

// TaskCreator abstracts creating and running scheduled tasks so the
// executor does not depend on the task package directly.
type TaskCreator interface {
	CreateTask(name, prompt, cronExpr, modelTier string, notifyChat bool, userID string, notifyUsers []string) (int64, error)
	UpdateTask(id int64, prompt, cronExpr, modelTier *string, notifyChat *bool, notifyUsers *[]string) error
	ListTasks(userID string) ([]map[string]any, error)
	RunTask(ctx context.Context, id int64) (map[string]any, error)
	DeleteTask(id int64) error
	ToggleTask(id int64, enabled bool) error
	HealTask(ctx context.Context, id int64, reason string) (string, error)
}

// MCPManager handles tool execution for MCP servers.
type MCPManager interface {
	CallTool(ctx context.Context, serverName, toolName string, args json.RawMessage) (string, error)
	StartServer(ctx context.Context, name string) error
	ServerNames() []string
}

// ConnectorCaller dispatches tool calls to connector plugins.
type ConnectorCaller interface {
	IsConnectorTool(name string) bool
	CallTool(ctx context.Context, qualifiedName, argsJSON, userID string) (string, error)
}

// BrowserConnector provides browser control via Chrome DevTools Protocol.
type BrowserConnector interface {
	IsEnabled() bool
	Execute(ctx context.Context, name, args string) (string, error)
}

// UserInfo is a minimal user representation returned by list_users.
type UserInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserLister provides user listing for the list_users and notify_user tools.
type UserLister interface {
	ListOtherUsers(callerID string) ([]UserInfo, error)
	ListAllUsers() ([]UserInfo, error)
}

// SkillManager abstracts skill operations so the executor does not
// depend on the skills package directly.
type SkillManager interface {
	Search(ctx context.Context, query string) ([]map[string]any, error)
	Install(ctx context.Context, slug string, force bool) (map[string]any, error)
	CreateSkill(slug, name, summary, content string) (map[string]any, error)
	List() ([]map[string]any, error)
	ReadSkill(nodeID string) (string, error)
	UpdateSkill(nodeID, title, summary, content string) (map[string]any, error)
}

// MemoryWriter abstracts creating memory nodes so the executor does not
// depend on the memory package directly.
type MemoryWriter interface {
	SaveMemory(title, content string, pinned bool, userID, subjectID *string) (string, error)
}

// MemoryPrivacyToggler toggles memory node privacy.
type MemoryPrivacyToggler interface {
	ToggleMemoryPrivacy(nodeID string, private bool, callerID string) error
}

// UserNotifier sends ad-hoc notifications to other users.
type UserNotifier interface {
	NotifyUser(senderID, senderName, recipientID, message string) error
}

// DomainAllowlister persists domain allowlist changes to the config store
// and returns the merged list. Implemented by config.Store.
type DomainAllowlister interface {
	MergeAllowedDomains(domains []string) ([]string, error)
}

// Executor dispatches tool calls to their implementations.
type Executor struct {
	registry           *Registry
	workspaceDir       string
	shellDir           string // working dir for shell commands; defaults to workspaceDir
	runner             sandbox.Runner
	taskCreator        TaskCreator
	skillManager       SkillManager
	memoryWriter       MemoryWriter
	domainAllowlister  DomainAllowlister
	mcpManager         MCPManager
	connectorCaller    ConnectorCaller
	browserConnector   BrowserConnector
	userLister         UserLister
	memoryToggler      MemoryPrivacyToggler
	userNotifier       UserNotifier
	httpClient         *http.Client
	searchCounter      uint64
	logger             *slog.Logger
	audit              security.AuditLogger
	sensitivePaths     []string
	dangerousCommands  []string
	allowedDomains     []string
}

// NewExecutor creates a tool executor backed by the given registry.
// workspaceDir is the root directory for file operations.
// audit may be nil (audit logging is skipped). sensitivePaths may use ~ prefixes.
// dangerousCommands may be nil (defaults to security.DefaultDangerousCommands).
func NewExecutor(registry *Registry, workspaceDir string, runner sandbox.Runner, taskCreator TaskCreator, skillManager SkillManager, memoryWriter MemoryWriter, logger *slog.Logger, audit security.AuditLogger, sensitivePaths []string, dangerousCommands []string, allowedDomains []string) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	if runner == nil {
		runner = sandbox.NewHostRunner(0, logger)
	}
	if len(sensitivePaths) == 0 {
		sensitivePaths = security.DefaultSensitivePaths
	}
	if len(dangerousCommands) == 0 {
		dangerousCommands = security.DefaultDangerousCommands
	}
	return &Executor{
		registry:          registry,
		workspaceDir:      workspaceDir,
		runner:            runner,
		taskCreator:       taskCreator,
		skillManager:      skillManager,
		memoryWriter:      memoryWriter,
		httpClient: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= fetchMaxRedirects {
					return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
				}
				return nil
			},
		},
		logger:            logger,
		audit:             audit,
		sensitivePaths:    sensitivePaths,
		dangerousCommands: dangerousCommands,
		allowedDomains:    allowedDomains,
	}
}

// SetAllowedDomains hot-swaps the domain allowlist used by the network
// command filter. Pass nil to revert to blocking all network commands.
func (e *Executor) SetAllowedDomains(domains []string) {
	e.allowedDomains = domains
}

// SetMCPManager wires the MCP tool dispatch layer.
func (e *Executor) SetMCPManager(m MCPManager) {
	e.mcpManager = m
}

// SetDomainAllowlister wires the config-backed domain persistence layer.
func (e *Executor) SetDomainAllowlister(da DomainAllowlister) {
	e.domainAllowlister = da
}

// SetConnectorCaller wires the connector tool dispatch layer.
func (e *Executor) SetConnectorCaller(c ConnectorCaller) { e.connectorCaller = c }

// SetBrowserConnector wires the browser connector for CDP-based browser tools.
func (e *Executor) SetBrowserConnector(b BrowserConnector) { e.browserConnector = b }

// SetUserLister wires the user listing layer.
func (e *Executor) SetUserLister(ul UserLister) { e.userLister = ul }

// SetMemoryToggler wires the memory privacy toggle layer.
func (e *Executor) SetMemoryToggler(mt MemoryPrivacyToggler) { e.memoryToggler = mt }

// SetUserNotifier wires the user notification layer.
func (e *Executor) SetUserNotifier(un UserNotifier) { e.userNotifier = un }

// SetShellDir sets the working directory for shell commands. This should
// be a subdirectory of the workspace so that config and secrets files in
// the workspace root are not reachable via relative paths or globs.
func (e *Executor) SetShellDir(dir string) {
	e.shellDir = dir
}

// effectiveShellDir returns the directory shell commands should run in.
func (e *Executor) effectiveShellDir() string {
	if e.shellDir != "" {
		return e.shellDir
	}
	return e.workspaceDir
}

// networkBlockHint returns the actionable suffix for network-blocked error
// messages. During interactive chat the agent can ask the user and call
// allow_domain; during autonomous task execution there is no user to ask.
func networkBlockHint(ctx context.Context) string {
	if task.ToolCallCollectorFromContext(ctx) != nil {
		return "This domain must be allowlisted before the task runs. Re-install the skill or add the domain via the dashboard"
	}
	return "Ask the user to approve this domain, then call allow_domain to add it"
}

func (e *Executor) logAudit(ctx context.Context, action, tool, target, outcome, reason string) {
	if e.audit == nil {
		return
	}
	var uid *string
	if u, ok := auth.UserFromContext(ctx); ok && u.ID != "" {
		uid = &u.ID
	}
	_ = e.audit.LogAudit(ctx, security.AuditEvent{
		Action:  action,
		Tool:    tool,
		Target:  target,
		Outcome: outcome,
		Reason:  reason,
		UserID:  uid,
	})
}

// Execute runs the named tool with the given JSON arguments.
func (e *Executor) Execute(ctx context.Context, name string, arguments string) (string, error) {
	switch name {
	case "read_file":
		return e.readFile(ctx, arguments)
	case "write_file":
		return e.writeFile(ctx, arguments)
	case "list_directory":
		return e.listDirectory(arguments)
	case "shell":
		return e.shell(ctx, arguments)
	case "create_task":
		return e.createTask(ctx, arguments)
	case "list_tasks":
		return e.listTasks(ctx)
	case "run_task":
		return e.runTask(ctx, arguments)
	case "update_task":
		return e.updateTask(ctx, arguments)
	case "delete_task":
		return e.deleteTask(arguments)
	case "toggle_task":
		return e.toggleTask(arguments)
	case "heal_task":
		return e.healTask(ctx, arguments)
	case "search_skills":
		return e.searchSkills(ctx, arguments)
	case "install_skill":
		return e.installSkill(ctx, arguments)
	case "create_skill":
		return e.createSkill(arguments)
	case "list_installed_skills":
		return e.listInstalledSkills()
	case "read_skill":
		return e.readSkill(arguments)
	case "update_skill":
		return e.updateSkill(arguments)
	case "save_memory":
		return e.saveMemory(ctx, arguments)
	case "allow_domain":
		return e.allowDomain(ctx, arguments)
	case "fetch_url":
		return e.fetchURL(ctx, arguments)
	case "web_search":
		return e.webSearch(ctx, arguments)
	case "list_users":
		return e.listUsers(ctx)
	case "notify_user":
		return e.notifyUser(ctx, arguments)
	case "toggle_memory_privacy":
		return e.toggleMemoryPrivacy(ctx, arguments)
	case "start_mcp_server":
		return e.startMCPServer(ctx, arguments)
	default:
		// Browser connector tools (browser_* prefix).
		if strings.HasPrefix(name, "browser_") {
			if e.browserConnector == nil || !e.browserConnector.IsEnabled() {
				return "", fmt.Errorf("browser connector is not enabled")
			}
			return e.browserConnector.Execute(ctx, name, arguments)
		}
		// Check if it's a connector tool.
		if e.connectorCaller != nil && e.connectorCaller.IsConnectorTool(name) {
			var userID string
			if scope, ok := ChatScopeFromContext(ctx); ok && scope.UserID != "" {
				userID = scope.UserID
			}
			return e.connectorCaller.CallTool(ctx, name, arguments, userID)
		}
		def, ok := e.registry.Get(name)
		if ok && def.MCPServer != "" && e.mcpManager != nil {
			return e.mcpManager.CallTool(ctx, def.MCPServer, def.MCPToolName, json.RawMessage(arguments))
		}
		if !ok {
			// If the tool follows the mcp__server__tool pattern but isn't
			// registered yet (server hasn't started), parse the name and
			// call the MCP manager directly. CallTool will lazy-start the
			// server, which triggers tool registration for future calls.
			if e.mcpManager != nil && strings.HasPrefix(name, "mcp__") {
				rest := name[len("mcp__"):]
				if idx := strings.Index(rest, "__"); idx > 0 {
					serverName := rest[:idx]
					toolName := rest[idx+2:]
					if toolName != "" {
						return e.mcpManager.CallTool(ctx, serverName, toolName, json.RawMessage(arguments))
					}
				}
			}
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		if def.Command != "" {
			return e.runCommand(ctx, def, arguments)
		}
		return "", fmt.Errorf("tool %q has no executor", name)
	}
}


func (e *Executor) safePath(rel string) (string, error) {
	// Use filepath.Abs to normalize both paths so relative workspace dirs
	// (e.g. "./data") work correctly with filepath.Join (which strips "./").
	base, _ := filepath.Abs(e.workspaceDir)
	abs, _ := filepath.Abs(filepath.Join(e.workspaceDir, filepath.Clean(rel)))
	if !strings.HasPrefix(abs, base) {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	return abs, nil
}

func (e *Executor) readFile(ctx context.Context, args string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	abs, err := e.safePath(p.Path)
	if err != nil {
		return "", err
	}
	if blocked, pattern := security.IsSensitivePath(abs, e.sensitivePaths); blocked {
		e.logAudit(ctx, "tool_blocked", "read_file", abs, "blocked", "sensitive path: "+pattern)
		return "", fmt.Errorf("access denied: %s is a sensitive path", pattern)
	}
	e.logAudit(ctx, "file_read", "read_file", p.Path, "allowed", "")
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (e *Executor) writeFile(ctx context.Context, args string) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	abs, err := e.safePath(p.Path)
	if err != nil {
		return "", err
	}
	if blocked, pattern := security.IsSensitivePath(abs, e.sensitivePaths); blocked {
		e.logAudit(ctx, "tool_blocked", "write_file", abs, "blocked", "sensitive path: "+pattern)
		return "", fmt.Errorf("access denied: %s is a sensitive path", pattern)
	}
	e.logAudit(ctx, "file_write", "write_file", p.Path, "allowed", "")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(p.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(p.Content), p.Path), nil
}

func (e *Executor) listDirectory(args string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	abs, err := e.safePath(p.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, entry := range entries {
		suffix := ""
		if entry.IsDir() {
			suffix = "/"
		}
		lines = append(lines, entry.Name()+suffix)
	}
	return strings.Join(lines, "\n"), nil
}

func (e *Executor) shell(ctx context.Context, args string) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if blocked, pattern := security.ContainsSensitivePath(p.Command, e.sensitivePaths); blocked {
		e.logAudit(ctx, "tool_blocked", "shell", p.Command, "blocked", "references sensitive path: "+pattern)
		return "", fmt.Errorf("command blocked: references sensitive path %s", pattern)
	}

	if blocked, cmd := security.ContainsDangerousCommand(p.Command, e.dangerousCommands, e.allowedDomains); blocked {
		if security.NetworkCommands[cmd] {
			hint := networkBlockHint(ctx)
			if hosts := security.ExtractHosts(p.Command); len(hosts) > 0 {
				msg := fmt.Sprintf("network access blocked: %s not in the allowed domains list. %s",
					strings.Join(hosts, ", "), hint)
				e.logAudit(ctx, "tool_blocked", "shell", p.Command, "blocked", msg)
				return "", fmt.Errorf("%s", msg)
			}
			msg := fmt.Sprintf("network access blocked: %s requires a URL targeting an allowed domain, but no domain was found in the command", cmd)
			e.logAudit(ctx, "tool_blocked", "shell", p.Command, "blocked", msg)
			return "", fmt.Errorf("%s", msg)
		}
		e.logAudit(ctx, "tool_blocked", "shell", p.Command, "blocked", "dangerous command: "+cmd)
		return "", fmt.Errorf("command blocked: %s is not allowed", cmd)
	}

	e.logger.Info("shell command", "command", p.Command)
	e.logAudit(ctx, "shell_exec", "shell", p.Command, "allowed", "")

	res := e.runner.Run(ctx, sandbox.RunConfig{
		Command:    p.Command,
		WorkingDir: e.effectiveShellDir(),
		NeedsNet:   commandNeedsNet(p.Command),
	})
	result := res.Output
	if res.Err != nil {
		if result == "" {
			result = "(no output)"
		}
		return fmt.Sprintf("%s\n\nExit error: %v", result, res.Err),
			fmt.Errorf("exit code %d", res.ExitCode)
	}
	if result == "" {
		return "(command produced no output)", nil
	}
	return result, nil
}

// commandNeedsNet returns true if the command invokes a known network binary
// (from the allowlist). This lets the sandbox Runner open networking
// only when a previously-approved network command is being executed.
func commandNeedsNet(cmdStr string) bool {
	for _, tok := range security.Tokenize(cmdStr) {
		base := filepath.Base(tok)
		if security.NetworkCommands[base] {
			return true
		}
	}
	return false
}

func (e *Executor) createTask(ctx context.Context, args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		Name        string   `json:"name"`
		Prompt      string   `json:"prompt"`
		CronExpr    string   `json:"cron_expr"`
		ModelTier   string   `json:"model_tier"`
		NotifyChat  *bool    `json:"notify_chat"`
		NotifyUsers []string `json:"notify_users"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.ModelTier == "" {
		p.ModelTier = "cheap"
	}
	notifyChat := true
	if p.NotifyChat != nil {
		notifyChat = *p.NotifyChat
	}
	var userID string
	if scope, ok := ChatScopeFromContext(ctx); ok && scope.UserID != "" {
		userID = scope.UserID
	}
	var notifyUserIDs []string
	if len(p.NotifyUsers) > 0 && e.userLister != nil {
		allUsers, err := e.userLister.ListAllUsers()
		if err != nil {
			return "", fmt.Errorf("failed to list users: %w", err)
		}
		nameToID := make(map[string]string, len(allUsers))
		for _, u := range allUsers {
			nameToID[u.Name] = u.ID
		}
		for _, name := range p.NotifyUsers {
			if name == "everyone" {
				notifyUserIDs = []string{"*"}
				break
			}
			id, ok := nameToID[name]
			if !ok {
				return "", fmt.Errorf("no user found matching '%s'", name)
			}
			notifyUserIDs = append(notifyUserIDs, id)
		}
	}
	if _, err := e.taskCreator.CreateTask(p.Name, p.Prompt, p.CronExpr, p.ModelTier, notifyChat, userID, notifyUserIDs); err != nil {
		return "", fmt.Errorf("failed to create task: %w", err)
	}
	return fmt.Sprintf("Task created: %s", p.Name), nil
}

func (e *Executor) updateTask(ctx context.Context, args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		TaskID      int64     `json:"task_id"`
		Prompt      *string   `json:"prompt"`
		CronExpr    *string   `json:"cron_expr"`
		ModelTier   *string   `json:"model_tier"`
		NotifyChat  *bool     `json:"notify_chat"`
		NotifyUsers *[]string `json:"notify_users"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.TaskID <= 0 {
		return "", fmt.Errorf("task_id must be a positive integer, got %d", p.TaskID)
	}
	var notifyUserIDs *[]string
	if p.NotifyUsers != nil && e.userLister != nil {
		allUsers, err := e.userLister.ListAllUsers()
		if err != nil {
			return "", fmt.Errorf("failed to list users: %w", err)
		}
		nameToID := make(map[string]string, len(allUsers))
		for _, u := range allUsers {
			nameToID[u.Name] = u.ID
		}
		ids := make([]string, 0, len(*p.NotifyUsers))
		for _, name := range *p.NotifyUsers {
			if name == "everyone" {
				ids = []string{"*"}
				break
			}
			id, ok := nameToID[name]
			if !ok {
				return "", fmt.Errorf("no user found matching '%s'", name)
			}
			ids = append(ids, id)
		}
		notifyUserIDs = &ids
	}
	if err := e.taskCreator.UpdateTask(p.TaskID, p.Prompt, p.CronExpr, p.ModelTier, p.NotifyChat, notifyUserIDs); err != nil {
		return "", fmt.Errorf("failed to update task %d: %w", p.TaskID, err)
	}
	return fmt.Sprintf("Task %d updated.", p.TaskID), nil
}

func (e *Executor) listTasks(ctx context.Context) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var userID string
	if scope, ok := ChatScopeFromContext(ctx); ok && scope.UserID != "" {
		userID = scope.UserID
	}
	tasks, err := e.taskCreator.ListTasks(userID)
	if err != nil {
		return "", err
	}
	if len(tasks) == 0 {
		return "No scheduled tasks.", nil
	}
	data, _ := json.MarshalIndent(tasks, "", "  ")
	return string(data), nil
}

func (e *Executor) runTask(ctx context.Context, args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.TaskID <= 0 {
		return "", fmt.Errorf("task_id must be a positive integer, got %d", p.TaskID)
	}
	result, err := e.taskCreator.RunTask(ctx, p.TaskID)
	if err != nil {
		return "", fmt.Errorf("failed to run task: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (e *Executor) deleteTask(args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.TaskID <= 0 {
		return "", fmt.Errorf("task_id must be a positive integer, got %d", p.TaskID)
	}
	if err := e.taskCreator.DeleteTask(p.TaskID); err != nil {
		return "", fmt.Errorf("failed to delete task %d: %w", p.TaskID, err)
	}
	return fmt.Sprintf("Deleted task %d.", p.TaskID), nil
}

func (e *Executor) toggleTask(args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		TaskID  int64 `json:"task_id"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.TaskID <= 0 {
		return "", fmt.Errorf("task_id must be a positive integer, got %d", p.TaskID)
	}
	if err := e.taskCreator.ToggleTask(p.TaskID, p.Enabled); err != nil {
		return "", fmt.Errorf("failed to toggle task %d: %w", p.TaskID, err)
	}
	state := "disabled"
	if p.Enabled {
		state = "enabled"
	}
	return fmt.Sprintf("Task %d is now %s.", p.TaskID, state), nil
}

func (e *Executor) healTask(ctx context.Context, args string) (string, error) {
	if e.taskCreator == nil {
		return "", fmt.Errorf("task scheduling is not available")
	}
	var p struct {
		TaskID int64  `json:"task_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.TaskID <= 0 {
		return "", fmt.Errorf("task_id must be a positive integer, got %d", p.TaskID)
	}
	result, err := e.taskCreator.HealTask(ctx, p.TaskID, p.Reason)
	if err != nil {
		return "", fmt.Errorf("failed to heal task %d: %w", p.TaskID, err)
	}
	return result, nil
}

func (e *Executor) searchSkills(ctx context.Context, args string) (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	results, err := e.skillManager.Search(ctx, p.Query)
	if err != nil {
		return "", fmt.Errorf("skill search failed: %w", err)
	}
	if len(results) == 0 {
		return "No skills found matching your query.", nil
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	return string(data), nil
}

func (e *Executor) installSkill(ctx context.Context, args string) (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}

	// Block skill installation during autonomous task execution (no human in the loop).
	if task.ToolCallCollectorFromContext(ctx) != nil {
		return "", fmt.Errorf("skill installation is not allowed during task execution")
	}

	var p struct {
		Slug  string `json:"slug"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	e.logger.Info("install_skill", "slug", p.Slug, "force", p.Force)

	result, err := e.skillManager.Install(ctx, p.Slug, p.Force)
	if err != nil {
		return "", fmt.Errorf("skill install failed: %w", err)
	}
	e.logAudit(ctx, "skill_install", "install_skill", p.Slug, "allowed", "")
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (e *Executor) createSkill(args string) (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}
	var p struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	result, err := e.skillManager.CreateSkill(p.Slug, p.Name, p.Summary, p.Content)
	if err != nil {
		return "", fmt.Errorf("skill creation failed: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (e *Executor) listInstalledSkills() (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}
	skills, err := e.skillManager.List()
	if err != nil {
		return "", err
	}
	if len(skills) == 0 {
		return "No skills installed.", nil
	}
	data, _ := json.MarshalIndent(skills, "", "  ")
	return string(data), nil
}

func (e *Executor) readSkill(args string) (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}
	var p struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	return e.skillManager.ReadSkill(p.NodeID)
}

func (e *Executor) updateSkill(args string) (string, error) {
	if e.skillManager == nil {
		return "", fmt.Errorf("skill management is not available")
	}
	var p struct {
		NodeID  string `json:"node_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	result, err := e.skillManager.UpdateSkill(p.NodeID, "", "", p.Content)
	if err != nil {
		return "", fmt.Errorf("skill update failed: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (e *Executor) saveMemory(ctx context.Context, args string) (string, error) {
	if e.memoryWriter == nil {
		return "", fmt.Errorf("memory is not available")
	}
	var p struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		Pinned    bool   `json:"pinned"`
		SubjectID string `json:"subject_id"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// Determine node ownership from chat scope.
	// Private sessions create user-owned nodes; standard sessions create shared nodes.
	var userID *string
	if scope, ok := ChatScopeFromContext(ctx); ok && scope.Private && scope.UserID != "" {
		uid := scope.UserID
		userID = &uid
	}

	// Default subject to the current user when no explicit subject_id is
	// provided. This ensures every memory knows who it is about, so
	// retrieval can correctly attribute facts to the right person.
	// Non-admin users can only save memories about themselves.
	var subjectID *string
	if p.SubjectID != "" {
		if scope, ok := ChatScopeFromContext(ctx); ok && scope.UserID != "" {
			if p.SubjectID != scope.UserID && scope.Role != "admin" {
				return "", fmt.Errorf("you can only save memories about yourself")
			}
		}
		subjectID = &p.SubjectID
	} else if scope, ok := ChatScopeFromContext(ctx); ok && scope.UserID != "" {
		uid := scope.UserID
		subjectID = &uid
	}

	nodeID, err := e.memoryWriter.SaveMemory(p.Title, p.Content, p.Pinned, userID, subjectID)
	if err != nil {
		return "", fmt.Errorf("failed to save memory: %w", err)
	}
	return fmt.Sprintf("Saved to memory (id=%s): %s", nodeID, p.Title), nil
}

func (e *Executor) allowDomain(ctx context.Context, args string) (string, error) {
	if e.domainAllowlister == nil {
		return "", fmt.Errorf("domain allowlisting is not available")
	}

	// Block during autonomous task execution (no human in the loop to approve).
	if task.ToolCallCollectorFromContext(ctx) != nil {
		return "", fmt.Errorf("domain allowlisting requires user approval and is not available during task execution")
	}

	var p struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.Domain == "" {
		return "", fmt.Errorf("domain is required")
	}

	merged, err := e.domainAllowlister.MergeAllowedDomains([]string{p.Domain})
	if err != nil {
		return "", fmt.Errorf("failed to allowlist domain: %w", err)
	}
	e.SetAllowedDomains(merged)
	e.logAudit(ctx, "domain_allowed", "allow_domain", p.Domain, "allowed", "")
	e.logger.Info("domain allowlisted", "domain", p.Domain)

	return fmt.Sprintf("Domain %s added to the allowlist. Network commands targeting this domain are now permitted.", p.Domain), nil
}

func (e *Executor) listUsers(ctx context.Context) (string, error) {
	if e.userLister == nil {
		return "", fmt.Errorf("user listing is not available")
	}
	var callerID string
	if scope, ok := ChatScopeFromContext(ctx); ok {
		callerID = scope.UserID
	}
	users, err := e.userLister.ListOtherUsers(callerID)
	if err != nil {
		return "", fmt.Errorf("failed to list users: %w", err)
	}
	if len(users) == 0 {
		return "No other users on this instance.", nil
	}
	data, _ := json.MarshalIndent(users, "", "  ")
	return string(data), nil
}

func (e *Executor) notifyUser(ctx context.Context, args string) (string, error) {
	if e.userNotifier == nil {
		return "", fmt.Errorf("user notifications are not available")
	}
	if e.userLister == nil {
		return "", fmt.Errorf("user listing is not available")
	}

	var p struct {
		UserName string `json:"user_name"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.UserName == "" {
		return "", fmt.Errorf("user_name is required")
	}
	if p.Message == "" {
		return "", fmt.Errorf("message is required")
	}

	// Resolve sender.
	var senderID, senderName string
	if scope, ok := ChatScopeFromContext(ctx); ok {
		senderID = scope.UserID
	}

	allUsers, err := e.userLister.ListAllUsers()
	if err != nil {
		return "", fmt.Errorf("failed to list users: %w", err)
	}

	// Find sender name.
	for _, u := range allUsers {
		if u.ID == senderID {
			senderName = u.Name
			break
		}
	}

	// Resolve recipient by exact name match.
	var matches []UserInfo
	for _, u := range allUsers {
		if u.Name == p.UserName {
			matches = append(matches, u)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no user found matching '%s'", p.UserName)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple users match '%s'; please use their full name", p.UserName)
	}

	recipient := matches[0]
	if err := e.userNotifier.NotifyUser(senderID, senderName, recipient.ID, p.Message); err != nil {
		return "", fmt.Errorf("failed to send notification: %w", err)
	}

	return fmt.Sprintf("Notification sent to %s.", p.UserName), nil
}

func (e *Executor) toggleMemoryPrivacy(ctx context.Context, args string) (string, error) {
	if e.memoryToggler == nil {
		return "", fmt.Errorf("memory privacy toggle is not available")
	}
	var p struct {
		MemoryID string `json:"memory_id"`
		Private  bool   `json:"private"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.MemoryID == "" {
		return "", fmt.Errorf("memory_id is required")
	}
	var callerID string
	if scope, ok := ChatScopeFromContext(ctx); ok {
		callerID = scope.UserID
	}
	if err := e.memoryToggler.ToggleMemoryPrivacy(p.MemoryID, p.Private, callerID); err != nil {
		return "", fmt.Errorf("failed to toggle memory privacy: %w", err)
	}
	status := "shared"
	if p.Private {
		status = "private"
	}
	return fmt.Sprintf("Memory %s is now %s.", p.MemoryID, status), nil
}

func (e *Executor) startMCPServer(ctx context.Context, args string) (string, error) {
	if e.mcpManager == nil {
		return "", fmt.Errorf("MCP is not configured")
	}
	var p struct {
		ServerName string `json:"server_name"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.ServerName == "" {
		return "", fmt.Errorf("server_name is required")
	}

	// Verify the server exists in the config.
	found := false
	for _, name := range e.mcpManager.ServerNames() {
		if name == p.ServerName {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("unknown MCP server %q", p.ServerName)
	}

	if err := e.mcpManager.StartServer(ctx, p.ServerName); err != nil {
		return "", fmt.Errorf("failed to start MCP server %q: %w", p.ServerName, err)
	}

	return fmt.Sprintf("MCP server %q started successfully. Its tools are now available.", p.ServerName), nil
}

func (e *Executor) runCommand(ctx context.Context, def ToolDef, args string) (string, error) {
	dir := e.effectiveShellDir()
	if def.WorkingDir != "" {
		abs, _ := filepath.Abs(def.WorkingDir)
		base, _ := filepath.Abs(e.workspaceDir)
		if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
			return "", fmt.Errorf("custom tool working_dir escapes workspace: %s", def.WorkingDir)
		}
		dir = abs
	}

	// Substitute arguments into the command template with shell escaping.
	cmdStr := def.Command
	var params map[string]any
	if err := json.Unmarshal([]byte(args), &params); err == nil {
		for k, v := range params {
			safe := "'" + strings.ReplaceAll(fmt.Sprint(v), "'", "'\\''") + "'"
			cmdStr = strings.ReplaceAll(cmdStr, "{{"+k+"}}", safe)
		}
	}

	if blocked, pattern := security.ContainsSensitivePath(cmdStr, e.sensitivePaths); blocked {
		e.logAudit(ctx, "tool_blocked", def.Name, cmdStr, "blocked", "references sensitive path: "+pattern)
		return "", fmt.Errorf("command blocked: references sensitive path %s", pattern)
	}

	if blocked, cmd := security.ContainsDangerousCommand(cmdStr, e.dangerousCommands, e.allowedDomains); blocked {
		if security.NetworkCommands[cmd] {
			hint := networkBlockHint(ctx)
			if hosts := security.ExtractHosts(cmdStr); len(hosts) > 0 {
				msg := fmt.Sprintf("network access blocked: %s not in the allowed domains list. %s",
					strings.Join(hosts, ", "), hint)
				e.logAudit(ctx, "tool_blocked", def.Name, cmdStr, "blocked", msg)
				return "", fmt.Errorf("%s", msg)
			}
			msg := fmt.Sprintf("network access blocked: %s requires a URL targeting an allowed domain, but no domain was found in the command", cmd)
			e.logAudit(ctx, "tool_blocked", def.Name, cmdStr, "blocked", msg)
			return "", fmt.Errorf("%s", msg)
		}
		e.logAudit(ctx, "tool_blocked", def.Name, cmdStr, "blocked", "dangerous command: "+cmd)
		return "", fmt.Errorf("command blocked: %s is not allowed", cmd)
	}
	e.logAudit(ctx, "shell_exec", def.Name, cmdStr, "allowed", "")

	res := e.runner.Run(ctx, sandbox.RunConfig{
		Command:    cmdStr,
		WorkingDir: dir,
		NeedsNet:   commandNeedsNet(cmdStr),
	})
	result := res.Output
	if res.Err != nil {
		return fmt.Sprintf("%s\n\nExit error: %v", result, res.Err), nil
	}
	return result, nil
}

// ResolveToolNames categorizes raw tool names into display-ready groups
// using the executor's tool registry.
func (e *Executor) ResolveToolNames(names []string) ResolvedTools {
	return Resolve(names, e.registry)
}
