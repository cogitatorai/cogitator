package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// Status constants for server lifecycle.
const (
	StatusStopped      = "stopped"
	StatusStarting     = "starting"
	StatusRunning      = "running"
	StatusReconnecting = "reconnecting"
	StatusError        = "error"
)

const maxReconnectAttempts = 5

// ToolInfo describes a single tool exposed by an MCP server.
type ToolInfo struct {
	Name          string         `json:"name"`
	QualifiedName string         `json:"qualified_name"`
	ServerName    string         `json:"server_name"`
	Description   string         `json:"description"`
	InputSchema   map[string]any `json:"input_schema"`
}

// ServerStatus is the external view of a server's state.
type ServerStatus struct {
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Command      string     `json:"command,omitempty"`
	Args         []string   `json:"args,omitempty"`
	URL          string     `json:"url,omitempty"`
	Transport    string     `json:"transport,omitempty"`
	Remote       bool       `json:"remote"`
	ToolCount    int        `json:"tool_count"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	Error        string     `json:"error,omitempty"`
	Instructions string     `json:"instructions,omitempty"`
}

// serverState holds the runtime state for one MCP server.
type serverState struct {
	config          ServerConfig
	status          string
	client          *mcpclient.Client
	tools           []ToolInfo
	startedAt       *time.Time
	lastError       string
	reconnectCancel context.CancelFunc // non-nil during reconnection
}

// Manager owns the lifecycle of all configured MCP server processes.
type Manager struct {
	mu         sync.RWMutex
	configPath string
	store      secretstore.SecretStore
	config     *Config
	servers    map[string]*serverState
	secrets    map[string]*ServerSecrets
	eventBus   *bus.Bus
	logger     *slog.Logger
	onToolsReg func()
}

// NewManager constructs a Manager. Call LoadConfig before using any other method.
func NewManager(configPath string, store secretstore.SecretStore, eventBus *bus.Bus, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		configPath: configPath,
		store:      store,
		servers:    map[string]*serverState{},
		eventBus:   eventBus,
		logger:     logger,
	}
}

// LoadConfig reads mcp.json and initialises all server states as stopped.
func (m *Manager) LoadConfig() error {
	cfg, err := LoadConfig(m.configPath)
	if err != nil {
		return fmt.Errorf("mcp: load config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg

	// Add any new servers from config; preserve existing runtime state.
	for name, sc := range cfg.Servers {
		if _, exists := m.servers[name]; !exists {
			m.servers[name] = &serverState{
				config: sc,
				status: StatusStopped,
			}
		}
	}

	// Remove servers that were dropped from config.
	for name := range m.servers {
		if _, exists := cfg.Servers[name]; !exists {
			delete(m.servers, name)
		}
	}

	// Load per-server secrets (best effort).
	secrets, err := LoadMCPSecrets(m.store)
	if err != nil {
		m.logger.Warn("mcp: failed to load secrets", "err", err)
		secrets = map[string]*ServerSecrets{}
	}
	m.secrets = secrets

	return nil
}

// Servers returns an external view of all configured servers.
func (m *Manager) Servers() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ServerStatus, 0, len(m.servers))
	for name, s := range m.servers {
		out = append(out, ServerStatus{
			Name:         name,
			Status:       s.status,
			Command:      s.config.Command,
			Args:         s.config.Args,
			URL:          s.config.URL,
			Transport:    s.config.Transport,
			Remote:       s.config.IsRemote(),
			ToolCount:    len(s.tools),
			StartedAt:    s.startedAt,
			Error:        s.lastError,
			Instructions: s.config.Instructions,
		})
	}
	return out
}

// ServerInstructions returns a map of server name to its configured
// instructions for all servers (including stopped ones).
func (m *Manager) ServerInstructions() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.servers))
	for name, s := range m.servers {
		out[name] = s.config.Instructions
	}
	return out
}

// ServerNames returns the names of all configured servers.
func (m *Manager) ServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// Tools returns the tools for a specific server, starting it if necessary.
func (m *Manager) Tools(ctx context.Context, serverName string) ([]ToolInfo, error) {
	m.mu.RLock()
	s, exists := m.servers[serverName]
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("mcp: unknown server %q", serverName)
	}
	if s.status == StatusRunning {
		tools := s.tools
		m.mu.RUnlock()
		return tools, nil
	}
	m.mu.RUnlock()

	if err := m.StartServer(ctx, serverName); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.servers[serverName].tools, nil
}

// AllTools returns tools from all servers that are currently running. It does
// not attempt to start stopped servers.
func (m *Manager) AllTools() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []ToolInfo
	for _, s := range m.servers {
		if s.status == StatusRunning {
			out = append(out, s.tools...)
		}
	}
	return out
}

// StartServer spawns the server process, performs the MCP handshake, and
// discovers available tools.
func (m *Manager) StartServer(ctx context.Context, name string) error {
	m.mu.Lock()
	s, exists := m.servers[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("mcp: unknown server %q", name)
	}
	if s.status == StatusRunning {
		m.mu.Unlock()
		return nil
	}
	s.status = StatusStarting
	s.lastError = ""
	cfg := s.config
	sec := m.secrets[name] // snapshot under lock
	m.mu.Unlock()

	m.publishState(name, StatusStarting)

	var client *mcpclient.Client

	if cfg.IsRemote() {
		// Merge headers from mcp.json and the secret store.
		var secretHeaders map[string]string
		if sec != nil {
			secretHeaders = sec.Headers
		}
		headers := mergeHeaders(cfg.Headers, secretHeaders)

		switch cfg.Transport {
		case "sse":
			var opts []transport.ClientOption
			if len(headers) > 0 {
				opts = append(opts, transport.WithHeaders(headers))
			}
			if sec != nil && sec.OAuth != nil {
				opts = append(opts, transport.WithOAuth(toOAuthConfig(sec.OAuth)))
			}
			var err2 error
			client, err2 = mcpclient.NewSSEMCPClient(cfg.URL, opts...)
			if err2 != nil {
				m.setError(name, err2)
				if strings.Contains(err2.Error(), "unexpected content type") {
					return fmt.Errorf("mcp: server %q endpoint returned a non-JSON response (check that the URL is a valid MCP endpoint and the remote service is available)", name)
				}
				return fmt.Errorf("mcp: create SSE client %q: %w", name, err2)
			}

		default: // "streamable-http" (validated by LoadConfig)
			var opts []transport.StreamableHTTPCOption
			if len(headers) > 0 {
				opts = append(opts, transport.WithHTTPHeaders(headers))
			}
			if sec != nil && sec.OAuth != nil {
				opts = append(opts, transport.WithHTTPOAuth(toOAuthConfig(sec.OAuth)))
			}
			var err2 error
			client, err2 = mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
			if err2 != nil {
				m.setError(name, err2)
				if strings.Contains(err2.Error(), "unexpected content type") {
					return fmt.Errorf("mcp: server %q endpoint returned a non-JSON response (check that the URL is a valid MCP endpoint and the remote service is available)", name)
				}
				return fmt.Errorf("mcp: create Streamable HTTP client %q: %w", name, err2)
			}
		}
	} else {
		// Stdio: build environment slice.
		var env []string
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		var err2 error
		client, err2 = mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
		if err2 != nil {
			m.setError(name, err2)
			return fmt.Errorf("mcp: start %q: %w", name, err2)
		}
	}

	_, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "cogitator",
				Version: "1.0.0",
			},
		},
	})
	if err != nil {
		_ = client.Close()
		m.setError(name, err)
		return fmt.Errorf("mcp: initialize %q: %w", name, err)
	}

	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		m.setError(name, err)
		return fmt.Errorf("mcp: list tools %q: %w", name, err)
	}

	tools := make([]ToolInfo, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema, _ := toolInputSchemaToMap(t.InputSchema)
		tools = append(tools, ToolInfo{
			Name:          t.Name,
			QualifiedName: QualifiedToolName(name, t.Name),
			ServerName:    name,
			Description:   t.Description,
			InputSchema:   schema,
		})
	}

	now := time.Now()
	m.mu.Lock()
	s.client = client
	s.status = StatusRunning
	s.tools = tools
	s.startedAt = &now
	s.lastError = ""
	m.mu.Unlock()

	m.logger.Info("mcp server started", "server", name, "tools", len(tools))
	m.publishState(name, StatusRunning)

	if m.onToolsReg != nil {
		m.onToolsReg()
	}

	return nil
}

// StopServer closes the client connection for a named server.
func (m *Manager) StopServer(name string) error {
	m.mu.Lock()
	s, exists := m.servers[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("mcp: unknown server %q", name)
	}
	// Cancel any in-progress reconnection.
	if s.reconnectCancel != nil {
		s.reconnectCancel()
		s.reconnectCancel = nil
	}
	client := s.client
	s.client = nil
	s.status = StatusStopped
	s.tools = nil
	s.startedAt = nil
	m.mu.Unlock()

	m.publishState(name, StatusStopped)

	if client != nil {
		return client.Close()
	}
	return nil
}

// StopAll shuts down all running servers.
func (m *Manager) StopAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.servers))
	for name, s := range m.servers {
		if s.status == StatusRunning || s.status == StatusReconnecting {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.StopServer(name); err != nil {
			m.logger.Warn("mcp: stop server error", "server", name, "err", err)
		}
	}
}

// CallTool invokes a named tool on the given server, starting it lazily if needed.
func (m *Manager) CallTool(ctx context.Context, serverName, toolName string, args json.RawMessage) (string, error) {
	m.mu.RLock()
	s, exists := m.servers[serverName]
	var status string
	if exists {
		status = s.status
	}
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("mcp: unknown server %q", serverName)
	}
	if status == StatusReconnecting {
		return "", fmt.Errorf("mcp: server %q is reconnecting, try again shortly", serverName)
	}
	if status != StatusRunning {
		if err := m.StartServer(ctx, serverName); err != nil {
			return "", err
		}
	}

	m.mu.RLock()
	client := m.servers[serverName].client
	m.mu.RUnlock()

	var arguments any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return "", fmt.Errorf("mcp: unmarshal args: %w", err)
		}
	}

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		// For remote servers, trigger reconnection on connection errors.
		m.mu.RLock()
		isRemote := m.servers[serverName].config.IsRemote()
		m.mu.RUnlock()
		if isRemote {
			m.startReconnect(serverName)
			// Provide a user-friendly message for content-type errors, which
			// indicate the remote endpoint returned an HTML error/maintenance
			// page instead of JSON.
			errStr := err.Error()
			if strings.Contains(errStr, "unexpected content type") {
				return "", fmt.Errorf("mcp: server %q returned a non-JSON response (the remote endpoint may be temporarily unavailable or returning an error page); reconnecting in the background", serverName)
			}
		}
		return "", fmt.Errorf("mcp: call tool %q on %q: %w", toolName, serverName, err)
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// AddServer adds a new server entry to the in-memory state and persists the config.
func (m *Manager) AddServer(name string, cfg ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.servers[name]; exists {
		return fmt.Errorf("mcp: server %q already exists", name)
	}

	m.config.Servers[name] = cfg
	m.servers[name] = &serverState{
		config: cfg,
		status: StatusStopped,
	}

	return SaveConfig(m.configPath, m.config)
}

// UpdateServer updates mutable fields of a server's config and persists.
// The server does not need to be restarted for instruction changes.
func (m *Manager) UpdateServer(name string, fn func(*ServerConfig)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("mcp: unknown server %q", name)
	}

	cfg := s.config
	fn(&cfg)
	s.config = cfg
	m.config.Servers[name] = cfg

	return SaveConfig(m.configPath, m.config)
}

// RemoveServer stops a server if running, removes it from state, and persists.
func (m *Manager) RemoveServer(name string) error {
	m.mu.RLock()
	s, exists := m.servers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("mcp: server %q not found", name)
	}

	// Stop outside the write lock to avoid holding it during I/O.
	if s.status == StatusRunning {
		if err := m.StopServer(name); err != nil {
			m.logger.Warn("mcp: stop before remove", "server", name, "err", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.servers, name)
	delete(m.config.Servers, name)

	return SaveConfig(m.configPath, m.config)
}

// SetToolRegistrationCallback registers a function called after tool discovery
// completes for any server.
func (m *Manager) SetToolRegistrationCallback(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onToolsReg = fn
}

// ServerSecrets returns the secrets for a named server, or nil if none.
func (m *Manager) ServerSecrets(name string) *ServerSecrets {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.secrets[name]
}

// SaveServerSecrets updates the secrets for a named server and persists to the secret store.
func (m *Manager) SaveServerSecrets(name string, secrets *ServerSecrets) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.secrets == nil {
		m.secrets = map[string]*ServerSecrets{}
	}
	m.secrets[name] = secrets

	return SaveMCPSecrets(m.store, m.secrets)
}

// mergeHeaders combines base headers from mcp.json with secret headers from
// the secret store. Secret values override base values for the same key.
func mergeHeaders(base, secret map[string]string) map[string]string {
	if len(base) == 0 && len(secret) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(secret))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range secret {
		merged[k] = v
	}
	return merged
}

// sanitizeToolNamePart replaces characters not in [a-zA-Z0-9_-] with
// underscores so the resulting tool name is valid for LLM provider APIs.
func sanitizeToolNamePart(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// QualifiedToolName builds the canonical "mcp__server__tool" identifier.
// Both parts are sanitized to only contain [a-zA-Z0-9_-].
func QualifiedToolName(serverName, toolName string) string {
	return "mcp__" + sanitizeToolNamePart(serverName) + "__" + sanitizeToolNamePart(toolName)
}

// ParseQualifiedToolName splits a qualified tool name back into server and tool
// components. Returns ok=false if the name does not follow the expected format.
func ParseQualifiedToolName(qualified string) (server, tool string, ok bool) {
	if !strings.HasPrefix(qualified, "mcp__") {
		return "", "", false
	}
	rest := qualified[len("mcp__"):]
	idx := strings.Index(rest, "__")
	if idx < 0 {
		return "", "", false
	}
	server = rest[:idx]
	tool = rest[idx+2:]
	if server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// reconnectDelay returns the backoff duration for a given attempt (0-indexed).
// Delays: 1s, 2s, 4s, 8s, 16s, 30s (capped).
func reconnectDelay(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// startReconnect spawns a background goroutine that attempts to reconnect a
// remote server with exponential backoff. It stops after maxReconnectAttempts
// consecutive failures or when the context is cancelled (e.g. StopServer).
func (m *Manager) startReconnect(name string) {
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	s, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		cancel()
		return
	}
	s.status = StatusReconnecting
	s.reconnectCancel = cancel
	m.mu.Unlock()

	m.publishState(name, StatusReconnecting)

	go func() {
		defer cancel()
		for attempt := 0; attempt < maxReconnectAttempts; attempt++ {
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay(attempt)):
			}

			m.logger.Info("mcp: reconnecting", "server", name, "attempt", attempt+1)

			// Close the old client if any.
			m.mu.Lock()
			if s, ok := m.servers[name]; ok && s.client != nil {
				s.client.Close()
				s.client = nil
			}
			m.mu.Unlock()

			// Attempt to start the server normally.
			if err := m.StartServer(ctx, name); err != nil {
				m.logger.Warn("mcp: reconnect attempt failed", "server", name, "attempt", attempt+1, "err", err)
				// Reset status back to reconnecting (StartServer sets error on failure).
				m.mu.Lock()
				if s, ok := m.servers[name]; ok {
					s.status = StatusReconnecting
					s.reconnectCancel = cancel
				}
				m.mu.Unlock()
				continue
			}

			// Success.
			m.logger.Info("mcp: reconnected", "server", name)
			m.mu.Lock()
			if s, ok := m.servers[name]; ok {
				s.reconnectCancel = nil
			}
			m.mu.Unlock()
			return
		}

		// All attempts exhausted.
		m.mu.Lock()
		if s, ok := m.servers[name]; ok {
			s.status = StatusError
			s.lastError = "reconnection failed after max retries"
			s.reconnectCancel = nil
		}
		m.mu.Unlock()
		m.publishState(name, StatusError, "reconnection failed after max retries")
		m.logger.Error("mcp: reconnection exhausted", "server", name)
	}()
}

// setError records an error state for the named server.
func (m *Manager) setError(name string, err error) {
	m.mu.Lock()
	if s, ok := m.servers[name]; ok {
		s.status = StatusError
		s.lastError = err.Error()
	}
	m.mu.Unlock()
	m.publishState(name, StatusError, err.Error())
}

// publishState emits an MCPServerStateChanged event if the bus is available.
func (m *Manager) publishState(name, status string, errMsgs ...string) {
	if m.eventBus == nil {
		return
	}
	var errMsg string
	if len(errMsgs) > 0 {
		errMsg = errMsgs[0]
	}
	m.eventBus.Publish(bus.Event{
		Type: bus.MCPServerStateChanged,
		Payload: map[string]any{
			"server_name": name,
			"status":      status,
			"error":       errMsg,
		},
	})
}

// toOAuthConfig converts our OAuthSecrets to mcp-go's transport.OAuthConfig.
func toOAuthConfig(o *OAuthSecrets) transport.OAuthConfig {
	return transport.OAuthConfig{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Scopes:       o.Scopes,
		RedirectURI:  o.RedirectURI,
		TokenStore:   transport.NewMemoryTokenStore(),
	}
}

// toolInputSchemaToMap converts a ToolInputSchema to a plain map for JSON serialisation.
func toolInputSchemaToMap(schema mcp.ToolInputSchema) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
