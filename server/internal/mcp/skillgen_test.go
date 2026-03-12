package mcp

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"French Gov", "french-gov"},
		{"my-server", "my-server"},
		{"GitHub", "github"},
		{"test_server.v2", "test-server-v2"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMCPSkillSlug(t *testing.T) {
	if got := MCPSkillSlug("French Gov"); got != "mcp-french-gov" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateSkillContent(t *testing.T) {
	tools := []ToolInfo{
		{
			Name:          "search",
			QualifiedName: "mcp__test__search",
			Description:   "Search for datasets",
			InputSchema:   map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		},
		{
			Name:          "fetch",
			QualifiedName: "mcp__test__fetch",
			Description:   "Fetch a dataset by ID",
			InputSchema:   map[string]any{},
		},
	}

	content := GenerateSkillContent("Test Server", "A test server for data.", tools)

	checks := []string{
		"name: mcp-test-server",
		"# Test Server (MCP Server)",
		"A test server for data.",
		"## Tools (2)",
		"### mcp__test__search",
		"Search for datasets",
		"```json",
		"### mcp__test__fetch",
		"Fetch a dataset by ID",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func TestGenerateSkillContent_NoInstructions(t *testing.T) {
	content := GenerateSkillContent("bare", "", []ToolInfo{
		{Name: "ping", QualifiedName: "mcp__bare__ping", Description: "Ping"},
	})

	if strings.Contains(content, "\n\n\n") {
		t.Error("double blank line when instructions empty")
	}
	if !strings.Contains(content, "## Tools (1)") {
		t.Error("missing tool section")
	}
}
