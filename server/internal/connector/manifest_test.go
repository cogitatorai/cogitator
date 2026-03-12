package connector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest_Google(t *testing.T) {
	data := `
name: google
display_name: Google
description: Google Calendar and Gmail
version: 1.0.0

auth:
  type: oauth2
  auth_url: https://accounts.google.com/o/oauth2/auth
  token_url: https://oauth2.googleapis.com/token
  scopes:
    - https://www.googleapis.com/auth/calendar.readonly
    - https://www.googleapis.com/auth/gmail.readonly

tools:
  - name: calendar_list
    description: List calendar events
    parameters:
      - name: start_date
        type: string
        required: true
        description: "YYYY-MM-DD"
      - name: end_date
        type: string
        required: true
        description: "YYYY-MM-DD"
    request:
      method: GET
      url: "https://www.googleapis.com/calendar/v3/calendars/primary/events"
      query:
        timeMin: "{{.start_date}}T00:00:00Z"
        timeMax: "{{.end_date}}T23:59:59Z"
        singleEvents: "true"
        orderBy: startTime
        maxResults: "50"
    response:
      root: ".items"
      fields:
        id: ".id"
        summary: ".summary"
`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "google"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "google", "connector.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := ParseManifest(filepath.Join(dir, "google", "connector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "google" {
		t.Fatalf("name = %q, want google", m.Name)
	}
	if m.Auth.Type != "oauth2" {
		t.Fatalf("auth.type = %q, want oauth2", m.Auth.Type)
	}
	if len(m.Auth.Scopes) != 2 {
		t.Fatalf("scopes count = %d, want 2", len(m.Auth.Scopes))
	}
	if len(m.Tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(m.Tools))
	}
	tool := m.Tools[0]
	if tool.Name != "calendar_list" {
		t.Fatalf("tool.name = %q, want calendar_list", tool.Name)
	}
	if tool.QualifiedName() != "google_calendar_list" {
		t.Fatalf("qualified = %q, want google_calendar_list", tool.QualifiedName())
	}
	if len(tool.Parameters) != 2 {
		t.Fatalf("params = %d, want 2", len(tool.Parameters))
	}
	if tool.Request.Method != "GET" {
		t.Fatalf("method = %q, want GET", tool.Request.Method)
	}
	if tool.Response.Root != ".items" {
		t.Fatalf("root = %q, want .items", tool.Response.Root)
	}
}

func TestParseManifest_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad", "connector.yaml"), []byte(":::invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseManifest(filepath.Join(dir, "bad", "connector.yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseManifest_MissingName(t *testing.T) {
	data := `
display_name: No Name
auth:
  type: oauth2
  auth_url: https://example.com/auth
  token_url: https://example.com/token
tools: []
`
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "noname"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "noname", "connector.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseManifest(filepath.Join(dir, "noname", "connector.yaml"))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestToolDef_ToProviderSchema(t *testing.T) {
	tool := ToolManifest{
		Name:        "calendar_list",
		Description: "List events",
		Parameters: []ParamDef{
			{Name: "start_date", Type: "string", Required: true, Description: "Start"},
			{Name: "end_date", Type: "string", Required: true, Description: "End"},
			{Name: "query", Type: "string", Required: false, Description: "Filter"},
		},
		connectorName: "google",
	}
	schema := tool.ProviderSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties not a map")
	}
	if len(props) != 3 {
		t.Fatalf("props count = %d, want 3", len(props))
	}
	req, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required not a []string")
	}
	if len(req) != 2 {
		t.Fatalf("required count = %d, want 2", len(req))
	}
}
