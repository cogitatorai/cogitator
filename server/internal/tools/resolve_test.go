package tools

import (
	"reflect"
	"testing"
)

func TestResolveEmpty(t *testing.T) {
	got := Resolve(nil, nil)
	if got.Skills != nil || got.Tools != nil || got.Memory != nil {
		t.Errorf("expected all nil for empty input, got %+v", got)
	}
}

func TestResolveBuiltinTools(t *testing.T) {
	got := Resolve([]string{"shell", "read_file", "write_file"}, nil)
	want := ResolvedTools{
		Tools: []string{"Read file", "Shell", "Write file"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveSkillTools(t *testing.T) {
	got := Resolve([]string{"read_skill", "search_skills", "install_skill"}, nil)
	want := ResolvedTools{
		Skills: []string{"Install skill", "Read skill", "Search skills"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveMemory(t *testing.T) {
	got := Resolve([]string{"save_memory"}, nil)
	want := ResolvedTools{
		Memory: []string{"Saved memory"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveMCPWithRegistry(t *testing.T) {
	r := &Registry{tools: map[string]ToolDef{
		"mcp__French_Gov__search_datasets": {MCPServer: "French Gov"},
	}}
	got := Resolve([]string{"mcp__French_Gov__search_datasets"}, r)
	want := ResolvedTools{
		Skills: []string{"French Gov (MCP)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveMCPFallback(t *testing.T) {
	// No registry entry: falls back to parsing the qualified name.
	got := Resolve([]string{"mcp__SomeServer__do_thing"}, nil)
	want := ResolvedTools{
		Skills: []string{"SomeServer (MCP)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveMCPGroupsByServer(t *testing.T) {
	// Multiple tools from the same MCP server should produce a single entry.
	got := Resolve([]string{"mcp__weather__forecast", "mcp__weather__current"}, nil)
	want := ResolvedTools{
		Skills: []string{"weather (MCP)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveMixed(t *testing.T) {
	r := &Registry{tools: map[string]ToolDef{
		"mcp__GitHub__list_repos": {MCPServer: "GitHub"},
	}}
	got := Resolve([]string{
		"shell", "read_file",
		"read_skill",
		"save_memory",
		"mcp__GitHub__list_repos",
		"unknown_custom_tool",
	}, r)
	want := ResolvedTools{
		Skills: []string{"GitHub (MCP)", "Read skill"},
		Tools:  []string{"Read file", "Shell", "unknown_custom_tool"},
		Memory: []string{"Saved memory"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestResolveDeduplicate(t *testing.T) {
	// Same tool called multiple times should appear once.
	got := Resolve([]string{"shell", "shell", "read_file", "shell"}, nil)
	want := ResolvedTools{
		Tools: []string{"Read file", "Shell"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
