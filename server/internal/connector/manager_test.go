package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func writeTestConnector(t *testing.T, dir, name, yaml string) {
	t.Helper()
	connDir := filepath.Join(dir, name)
	if err := os.MkdirAll(connDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(connDir, "connector.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

const testConnectorYAML = `
name: testapi
display_name: Test API
description: A test connector
version: 1.0.0

auth:
  type: oauth2
  auth_url: https://example.com/auth
  token_url: https://example.com/token
  scopes:
    - read

tools:
  - name: get_items
    description: Get items
    parameters:
      - name: query
        type: string
        required: true
    request:
      method: GET
      url: "PLACEHOLDER/items"
      query:
        q: "{{.query}}"
    response:
      root: ".items"
      fields:
        id: ".id"
        name: ".name"
`

func TestManager_LoadConnectors(t *testing.T) {
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", testConnectorYAML)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	connectors := m.List()
	if len(connectors) != 1 {
		t.Fatalf("connectors = %d, want 1", len(connectors))
	}
	if connectors[0].Name != "testapi" {
		t.Fatalf("name = %q", connectors[0].Name)
	}
}

func TestManager_ToolDefs(t *testing.T) {
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", testConnectorYAML)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	defs := m.ToolDefs()
	if len(defs) != 1 {
		t.Fatalf("tools = %d, want 1", len(defs))
	}
	if defs[0].Name != "testapi_get_items" {
		t.Fatalf("tool name = %q", defs[0].Name)
	}
}

func TestManager_CallTool_NotConnected(t *testing.T) {
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", testConnectorYAML)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	result, err := m.CallTool(context.Background(), "testapi_get_items", `{"query":"test"}`, "user1")
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// Should mention "not connected".
	if !containsSubstr(result, "not connected") {
		t.Fatalf("expected 'not connected' message, got: %s", result)
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestManager_CallTool_WithServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "1", "name": "Item One"},
			},
		})
	}))
	defer srv.Close()

	yaml := `
name: testapi
display_name: Test API
description: test
version: 1.0.0
auth:
  type: oauth2
  auth_url: https://example.com/auth
  token_url: https://example.com/token
  scopes: [read]
tools:
  - name: get_items
    description: Get items
    parameters:
      - name: query
        type: string
        required: true
    request:
      method: GET
      url: "` + srv.URL + `/items"
      query:
        q: "{{.query}}"
    response:
      root: ".items"
      fields:
        id: ".id"
        name: ".name"
`
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", yaml)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	// Manually inject a token so the connector appears connected.
	m.oauth.mu.Lock()
	m.oauth.tokens["testapi:user1"] = &TokenInfo{
		AccessToken:  "fake",
		RefreshToken: "fake",
	}
	m.oauth.mu.Unlock()

	// Override the HTTP client to use the test server's client (bypasses OAuth).
	m.SetClientOverride(func(connectorName, userID string) *http.Client {
		return srv.Client()
	})

	result, err := m.CallTool(context.Background(), "testapi_get_items", `{"query":"test"}`, "user1")
	if err != nil {
		t.Fatal(err)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(result), &items); err != nil {
		t.Fatalf("not valid JSON: %s", result)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0]["name"] != "Item One" {
		t.Fatalf("name = %v", items[0]["name"])
	}
}

func TestManager_IsConnectorTool(t *testing.T) {
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", testConnectorYAML)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	if !m.IsConnectorTool("testapi_get_items") {
		t.Fatal("expected testapi_get_items to be a connector tool")
	}
	if m.IsConnectorTool("nonexistent_tool") {
		t.Fatal("expected nonexistent_tool to not be a connector tool")
	}
}

func TestManager_CallTool_MultiCalendar(t *testing.T) {
	calendarHits := map[string][]map[string]any{
		"primary": {
			{"id": "evt1", "summary": "Personal event", "start": map[string]any{"dateTime": "2026-03-06T09:00:00Z"}, "end": map[string]any{"dateTime": "2026-03-06T10:00:00Z"}},
		},
		"team@group.calendar.google.com": {
			{"id": "evt2", "summary": "Team standup", "start": map[string]any{"date": "2026-03-06"}, "end": map[string]any{"date": "2026-03-07"}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract calendar ID from URL path: /calendar/v3/calendars/{id}/events
		parts := strings.SplitN(r.URL.Path, "/calendars/", 2)
		if len(parts) < 2 {
			http.Error(w, "bad path", 400)
			return
		}
		calID := strings.TrimSuffix(parts[1], "/events")
		items, ok := calendarHits[calID]
		if !ok {
			items = []map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer srv.Close()

	yaml := `
name: google
display_name: Google
description: test
version: 1.0.0
auth:
  type: oauth2
  auth_url: https://example.com/auth
  token_url: https://example.com/token
  scopes: [read]
tools:
  - name: calendar_list
    description: List events
    parameters:
      - name: start_date
        type: string
        required: true
      - name: end_date
        type: string
        required: true
    request:
      method: GET
      url: "` + srv.URL + `/calendar/v3/calendars/primary/events"
      query:
        timeMin: "{{.start_date}}T00:00:00Z"
        timeMax: "{{.end_date}}T23:59:59Z"
        singleEvents: "true"
        orderBy: startTime
    response:
      root: ".items"
      fields:
        id: ".id"
        summary: ".summary"
        start: ".start.dateTime // .start.date"
        end: ".end.dateTime // .end.date"
`
	dir := t.TempDir()
	writeTestConnector(t, dir, "google", yaml)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	// Inject token.
	m.oauth.mu.Lock()
	m.oauth.tokens["google:user1"] = &TokenInfo{AccessToken: "fake", RefreshToken: "fake"}
	m.oauth.mu.Unlock()
	m.SetClientOverride(func(_, _ string) *http.Client { return srv.Client() })

	// Set up enabled calendars.
	m.settings.SetCalendars("google", "user1", []CalendarEntry{
		{ID: "primary", Summary: "Andrei", Primary: true},
		{ID: "team@group.calendar.google.com", Summary: "Team", Primary: false},
	})
	m.settings.SetEnabledCalendarIDs("google", "user1", []string{"primary", "team@group.calendar.google.com"})

	result, err := m.CallTool(context.Background(), "google_calendar_list", `{"start_date":"2026-03-06","end_date":"2026-03-06"}`, "user1")
	if err != nil {
		t.Fatal(err)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(result), &items); err != nil {
		t.Fatalf("not valid JSON: %s", result)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2; result: %s", len(items), result)
	}

	// Verify calendar source is tagged on each event.
	for _, item := range items {
		cal, ok := item["calendar"].(string)
		if !ok || cal == "" {
			t.Fatalf("expected calendar field, got: %v", item)
		}
	}
}

func TestManager_ConnectorStatuses(t *testing.T) {
	dir := t.TempDir()
	writeTestConnector(t, dir, "testapi", testConnectorYAML)

	m := NewManager(dir, secretstore.NewFileStore(dir), filepath.Join(dir, "settings.yaml"), 8484)
	m.LoadAll()

	statuses := m.ConnectorStatuses("user1")
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if statuses["testapi"] {
		t.Fatal("expected testapi to be disconnected")
	}
}
