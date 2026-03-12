package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
	"github.com/cogitatorai/cogitator/server/internal/tools"
)

// ConnectorInfo is the public view of a loaded connector.
type ConnectorInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	HasAuth     bool   `json:"has_auth"`
	Trusted     bool   `json:"trusted"`
}

// Manager loads connector manifests, manages OAuth, and dispatches tool calls.
type Manager struct {
	mu             sync.RWMutex
	connectors     map[string]*Manifest
	connectorsDir  string
	oauth          *OAuthRuntime
	restExec       *RESTExecutor
	settings       *SettingsStore
	clientOverride func(connectorName, userID string) *http.Client // for testing
	logger         *slog.Logger
}

// NewManager creates a connector manager that loads manifests from dir.
func NewManager(connectorsDir string, store secretstore.SecretStore, settingsPath string, port int) *Manager {
	return &Manager{
		connectors:    make(map[string]*Manifest),
		connectorsDir: connectorsDir,
		oauth:         NewOAuthRuntime(store, port),
		restExec:      NewRESTExecutor(),
		settings:      NewSettingsStore(settingsPath),
		logger:        slog.Default(),
	}
}

// Settings returns the settings store (for API handlers).
func (m *Manager) Settings() *SettingsStore {
	return m.settings
}

// SetClientOverride sets a function that provides HTTP clients for testing,
// bypassing the OAuth flow.
func (m *Manager) SetClientOverride(fn func(connectorName, userID string) *http.Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientOverride = fn
}

// LoadAll scans the connectors directory and loads all valid manifests.
func (m *Manager) LoadAll() error {
	entries, err := os.ReadDir(m.connectorsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading connectors dir: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(m.connectorsDir, entry.Name(), "connector.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		}
		manifest, err := ParseManifest(manifestPath)
		if err != nil {
			m.logger.Warn("skipping connector", "dir", entry.Name(), "error", err)
			continue
		}
		m.connectors[manifest.Name] = manifest
	}
	return nil
}

// LoadEmbedded loads a manifest from embedded YAML content (for built-in connectors).
func (m *Manager) LoadEmbedded(name string, data []byte) error {
	dir, err := os.MkdirTemp("", "connector-embed-*")
	if err != nil {
		return err
	}
	connDir := filepath.Join(dir, name)
	if err := os.MkdirAll(connDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(connDir, "connector.yaml"), data, 0o644); err != nil {
		return err
	}
	manifest, err := ParseManifest(filepath.Join(connDir, "connector.yaml"))
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	manifest.Embedded = true
	// Workspace connectors override embedded ones.
	if _, exists := m.connectors[manifest.Name]; !exists {
		m.connectors[manifest.Name] = manifest
	}
	return nil
}

// List returns info about all loaded connectors.
func (m *Manager) List() []ConnectorInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ConnectorInfo, 0, len(m.connectors))
	for _, c := range m.connectors {
		result = append(result, ConnectorInfo{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Description: c.Description,
			Version:     c.Version,
			HasAuth:     c.Auth.Type != "",
			Trusted:     c.Embedded,
		})
	}
	return result
}

// ToolDefs returns tool definitions for all loaded connectors, suitable
// for registration in the tools.Registry.
func (m *Manager) ToolDefs() []tools.ToolDef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var defs []tools.ToolDef
	for _, c := range m.connectors {
		for _, t := range c.Tools {
			defs = append(defs, tools.ToolDef{
				Name:        t.QualifiedName(),
				Description: t.Description,
				Parameters:  t.ProviderSchema(),
				Builtin:     true,
			})
		}
	}
	return defs
}

// Status returns whether a user has a specific connector connected.
func (m *Manager) Status(connectorName, userID string) bool {
	return m.oauth.Status(connectorName, userID)
}

// StartAuth begins the OAuth flow for a connector. Returns the consent URL.
// clientID and clientSecret must be provided by the caller (from secrets).
func (m *Manager) StartAuth(connectorName, userID, clientID, clientSecret, redirectScheme, source, origin, callbackURL string) (string, error) {
	m.mu.RLock()
	c, ok := m.connectors[connectorName]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown connector: %s", connectorName)
	}
	return m.oauth.StartAuth(connectorName, userID, c.Auth, clientID, clientSecret, redirectScheme, source, origin, callbackURL)
}

// HandleCallback exchanges an auth code for tokens.
// Returns (connectorName, userID, redirectScheme, source, origin, error).
func (m *Manager) HandleCallback(code, state string) (string, string, string, string, string, error) {
	return m.oauth.HandleCallback(code, state)
}

// Revoke disconnects a connector for a user.
func (m *Manager) Revoke(connectorName, userID string) error {
	return m.oauth.Revoke(connectorName, userID)
}

// ConnectorStatuses returns the connection status of all connectors for a user.
func (m *Manager) ConnectorStatuses(userID string) map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make(map[string]bool, len(m.connectors))
	for name := range m.connectors {
		statuses[name] = m.oauth.Status(name, userID)
	}
	return statuses
}

// CallTool dispatches a tool call to the appropriate connector.
// Returns the result string (never a Go error for user-facing issues).
func (m *Manager) CallTool(ctx context.Context, qualifiedName, argsJSON, userID string) (string, error) {
	// Find the connector and tool from the qualified name.
	m.mu.RLock()
	var foundConnector *Manifest
	var foundTool *ToolManifest
	for _, c := range m.connectors {
		for i, t := range c.Tools {
			if t.QualifiedName() == qualifiedName {
				foundConnector = c
				foundTool = &c.Tools[i]
				break
			}
		}
		if foundTool != nil {
			break
		}
	}
	clientOverride := m.clientOverride
	m.mu.RUnlock()

	if foundTool == nil {
		return "", fmt.Errorf("unknown connector tool: %s", qualifiedName)
	}

	// Check connection status.
	if foundConnector.Auth.Type != "" && !m.oauth.Status(foundConnector.Name, userID) {
		m.logger.Warn("connector not connected for user", "connector", foundConnector.Name, "userID", userID)
		return fmt.Sprintf("%s not connected. Please connect %s in the Connectors page.",
			foundConnector.DisplayName, foundConnector.DisplayName), nil
	}

	// Get an authenticated HTTP client.
	var client *http.Client
	if clientOverride != nil {
		client = clientOverride(foundConnector.Name, userID)
	} else if foundConnector.Auth.Type != "" {
		var err error
		client, err = m.oauth.Client(foundConnector.Name, userID, foundConnector.Auth)
		if err != nil {
			return fmt.Sprintf("Authentication error: %s", err), nil
		}
	} else {
		client = http.DefaultClient
	}

	// Parse arguments.
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("Invalid arguments: %s", err), nil
		}
	}

	// Multi-calendar: fan out across enabled calendars.
	if isCalendarTool(qualifiedName) {
		enabledIDs := m.settings.GetEnabledCalendarIDs(foundConnector.Name, userID)
		// No calendar selection stored: auto-discover all calendars so we
		// never silently degrade to primary-only (e.g. tasks created before
		// auth, or users who never visited the Connectors settings page).
		if len(enabledIDs) == 0 {
			if cals, err := m.fetchAndCacheCalendars(client, foundConnector.Name, userID); err == nil && len(cals) > 0 {
				enabledIDs = make([]string, len(cals))
				for i, c := range cals {
					enabledIDs[i] = c.ID
				}
			}
		}
		if len(enabledIDs) > 1 {
			calEntries := m.settings.GetCalendars(foundConnector.Name, userID)
			return m.executeMultiCalendar(client, *foundTool, args, enabledIDs, calEntries)
		}
		// Single enabled calendar: rewrite the URL to use that calendar ID.
		if len(enabledIDs) == 1 && enabledIDs[0] != "primary" {
			tool := *foundTool
			tool.Request.URL = strings.Replace(tool.Request.URL, "/calendars/primary/", "/calendars/"+url.PathEscape(enabledIDs[0])+"/", 1)
			m.logger.Info("connector tool call", "tool", qualifiedName, "userID", userID, "args", args, "calendar", enabledIDs[0])
			result, err := m.restExec.Execute(client, tool, args)
			if err != nil {
				m.logger.Error("connector tool failed", "tool", qualifiedName, "error", err)
				return fmt.Sprintf("API error: %s", err), nil
			}
			m.logger.Info("connector tool result", "tool", qualifiedName, "resultLen", len(result))
			return result, nil
		}
	}

	// Execute the request.
	m.logger.Info("connector tool call", "tool", qualifiedName, "userID", userID, "args", args)
	result, err := m.restExec.Execute(client, *foundTool, args)
	if err != nil {
		m.logger.Error("connector tool failed", "tool", qualifiedName, "error", err)
		return fmt.Sprintf("API error: %s", err), nil
	}
	m.logger.Info("connector tool result", "tool", qualifiedName, "resultLen", len(result))
	return result, nil
}

// isCalendarTool returns true if this tool queries a calendar endpoint
// that should be fanned out across multiple calendars.
func isCalendarTool(qualifiedName string) bool {
	return qualifiedName == "google_calendar_list" || qualifiedName == "google_calendar_search"
}

// executeMultiCalendar fans out a calendar tool across multiple calendar IDs
// in parallel, merges results, deduplicates by event ID, and sorts by start time.
func (m *Manager) executeMultiCalendar(client *http.Client, tool ToolManifest, args map[string]any, calendarIDs []string, calEntries []CalendarEntry) (string, error) {
	// Build a lookup from calendar ID to human-readable summary.
	calNames := make(map[string]string, len(calEntries))
	for _, e := range calEntries {
		calNames[e.ID] = e.Summary
	}

	type calResult struct {
		items []map[string]any
		err   error
	}

	results := make([]calResult, len(calendarIDs))
	var wg sync.WaitGroup

	for i, calID := range calendarIDs {
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			calTool := tool
			calTool.Request.URL = strings.Replace(tool.Request.URL, "/calendars/primary/", "/calendars/"+url.PathEscape(id)+"/", 1)

			m.logger.Info("connector multi-cal request", "tool", tool.QualifiedName(), "calendar", id)
			raw, err := m.restExec.Execute(client, calTool, args)
			if err != nil {
				results[idx] = calResult{err: err}
				return
			}

			var items []map[string]any
			if err := json.Unmarshal([]byte(raw), &items); err != nil {
				results[idx] = calResult{err: err}
				return
			}

			// Tag each event with its source calendar.
			calName := calNames[id]
			if calName == "" {
				calName = id
			}
			for _, item := range items {
				item["calendar"] = calName
			}

			results[idx] = calResult{items: items}
		}(i, calID)
	}
	wg.Wait()

	// Merge and deduplicate by event ID.
	seen := make(map[string]bool)
	var merged []map[string]any
	for _, r := range results {
		if r.err != nil {
			m.logger.Warn("multi-cal partial failure", "error", r.err)
			continue
		}
		for _, item := range r.items {
			id, _ := item["id"].(string)
			if id != "" && seen[id] {
				continue
			}
			if id != "" {
				seen[id] = true
			}
			merged = append(merged, item)
		}
	}

	// Sort by start time.
	sort.Slice(merged, func(i, j int) bool {
		si, _ := merged[i]["start"].(string)
		sj, _ := merged[j]["start"].(string)
		return si < sj
	})

	if merged == nil {
		merged = []map[string]any{}
	}

	data, _ := json.MarshalIndent(merged, "", "  ")
	m.logger.Info("connector multi-cal result", "tool", tool.QualifiedName(), "calendars", len(calendarIDs), "events", len(merged))
	return string(data), nil
}

// FetchCalendarList fetches the user's calendar list from Google's CalendarList API,
// caches the result in settings, and returns the entries. On first fetch (no prior
// enabled calendars), all calendars are auto-enabled.
func (m *Manager) FetchCalendarList(connectorName, userID string) ([]CalendarEntry, error) {
	m.mu.RLock()
	c, ok := m.connectors[connectorName]
	clientOverride := m.clientOverride
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown connector: %s", connectorName)
	}

	var client *http.Client
	if clientOverride != nil {
		client = clientOverride(connectorName, userID)
	} else if c.Auth.Type != "" {
		var err error
		client, err = m.oauth.Client(connectorName, userID, c.Auth)
		if err != nil {
			return nil, err
		}
	} else {
		client = http.DefaultClient
	}

	return m.fetchAndCacheCalendars(client, connectorName, userID)
}

// fetchAndCacheCalendars queries the Google CalendarList API using the
// provided client, caches discovered calendars in settings, and auto-enables
// all calendars when no prior selection exists.
func (m *Manager) fetchAndCacheCalendars(client *http.Client, connectorName, userID string) ([]CalendarEntry, error) {
	body, err := doRequest(client, "GET", "https://www.googleapis.com/calendar/v3/users/me/calendarList")
	if err != nil {
		return nil, fmt.Errorf("fetching calendar list: %w", err)
	}

	items, err := queryJQ(body, ".items")
	if err != nil {
		return nil, fmt.Errorf("parsing calendar list: %w", err)
	}
	items = flattenArrayResults(items)

	var calendars []CalendarEntry
	for _, item := range items {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := im["id"].(string)
		summary, _ := im["summary"].(string)
		primary, _ := im["primary"].(bool)
		if id != "" {
			calendars = append(calendars, CalendarEntry{
				ID:      id,
				Summary: summary,
				Primary: primary,
			})
		}
	}

	if err := m.settings.SetCalendars(connectorName, userID, calendars); err != nil {
		return nil, err
	}

	// Auto-enable all calendars on first setup.
	existing := m.settings.GetEnabledCalendarIDs(connectorName, userID)
	if len(existing) == 0 && len(calendars) > 0 {
		ids := make([]string, len(calendars))
		for i, cal := range calendars {
			ids[i] = cal.ID
		}
		if err := m.settings.SetEnabledCalendarIDs(connectorName, userID, ids); err != nil {
			return nil, err
		}
	}

	return calendars, nil
}

// IsConnectorTool checks whether a tool name belongs to a loaded connector.
func (m *Manager) IsConnectorTool(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.connectors {
		for _, t := range c.Tools {
			if t.QualifiedName() == name {
				return true
			}
		}
	}
	return false
}

// OAuth returns the underlying OAuth runtime (for API handlers).
func (m *Manager) OAuth() *OAuthRuntime {
	return m.oauth
}

// Manifest returns the manifest for a named connector, if loaded.
func (m *Manager) Manifest(name string) (*Manifest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.connectors[name]
	return c, ok
}

