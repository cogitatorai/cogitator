package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRegistryBuiltins verifies that NewRegistry registers exactly the 15 built-in tools
// and that Get, List, and ProviderTools all reflect them correctly.
func TestRegistryBuiltins(t *testing.T) {
	r := NewRegistry("", nil)

	wantBuiltins := []string{
		"read_file", "write_file", "list_directory", "shell",
		"create_task", "list_tasks", "delete_task", "toggle_task", "run_task", "heal_task",
		"search_skills", "install_skill", "create_skill", "list_installed_skills", "read_skill",
		"update_skill", "start_mcp_server",
		"save_memory", "toggle_memory_privacy", "list_users",
	}

	// Verify Get returns each built-in.
	for _, name := range wantBuiltins {
		def, ok := r.Get(name)
		if !ok {
			t.Errorf("Get(%q): expected to find built-in tool, got not found", name)
			continue
		}
		if !def.Builtin {
			t.Errorf("Get(%q): Builtin flag expected true, got false", name)
		}
		if def.Name != name {
			t.Errorf("Get(%q): Name = %q, want %q", name, def.Name, name)
		}
	}

	// List should return all built-in tools.
	list := r.List()
	if len(list) != 24 {
		t.Errorf("List(): got %d tools, want 24", len(list))
	}

	// ProviderTools should return all built-in entries with non-empty names.
	pt := r.ProviderTools()
	if len(pt) != 24 {
		t.Errorf("ProviderTools(): got %d tools, want 24", len(pt))
	}
	for _, p := range pt {
		if p.Name == "" {
			t.Errorf("ProviderTools(): found entry with empty Name")
		}
		if p.Description == "" {
			t.Errorf("ProviderTools(): tool %q has empty Description", p.Name)
		}
		if p.Parameters == nil {
			t.Errorf("ProviderTools(): tool %q has nil Parameters", p.Name)
		}
	}
}

// TestRegistryLoadCustomTools verifies that a valid tool.yaml in the custom dir
// is loaded and visible through Get, List, and ProviderTools.
func TestRegistryLoadCustomTools(t *testing.T) {
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "web_search")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	yaml := `name: web_search
description: Search the web for information
parameters:
  type: object
  properties:
    query:
      type: string
      description: The search query
  required:
    - query
command: "curl -s 'https://api.example.com/search?q={{.query}}'"
`
	if err := os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := NewRegistry(dir, nil)
	if err := r.LoadCustomTools(); err != nil {
		t.Fatalf("LoadCustomTools: %v", err)
	}

	def, ok := r.Get("web_search")
	if !ok {
		t.Fatal("Get(\"web_search\"): expected tool to be loaded, got not found")
	}
	if def.Name != "web_search" {
		t.Errorf("Name = %q, want \"web_search\"", def.Name)
	}
	if def.Description == "" {
		t.Error("Description is empty")
	}
	if def.Command == "" {
		t.Error("Command is empty")
	}
	if def.Builtin {
		t.Error("custom tool should not have Builtin=true")
	}

	// List should now contain 24 tools (24 built-ins, custom web_search overrides the builtin).
	if got := len(r.List()); got != 24 {
		t.Errorf("List(): got %d tools, want 24", got)
	}

	// ProviderTools should also reflect the custom tool.
	found := false
	for _, p := range r.ProviderTools() {
		if p.Name == "web_search" {
			found = true
		}
	}
	if !found {
		t.Error("ProviderTools(): web_search not found")
	}
}

// TestRegistryDeleteCustom verifies that a registered custom tool can be deleted.
func TestRegistryDeleteCustom(t *testing.T) {
	r := NewRegistry("", nil)
	r.Register(ToolDef{
		Name:        "my_custom",
		Description: "A custom tool",
	})

	if _, ok := r.Get("my_custom"); !ok {
		t.Fatal("custom tool not found after Register")
	}

	if deleted := r.Delete("my_custom"); !deleted {
		t.Error("Delete(\"my_custom\"): expected true, got false")
	}

	if _, ok := r.Get("my_custom"); ok {
		t.Error("tool still present after Delete")
	}
}

// TestRegistryDeleteBuiltinFails verifies that Delete returns false for built-in
// tools and leaves them intact.
func TestRegistryDeleteBuiltinFails(t *testing.T) {
	r := NewRegistry("", nil)

	if deleted := r.Delete("read_file"); deleted {
		t.Error("Delete(\"read_file\"): expected false for built-in, got true")
	}

	if _, ok := r.Get("read_file"); !ok {
		t.Error("built-in read_file was removed after failed Delete")
	}
}

// TestRegistryLoadSkipsMissingYaml verifies that directories without tool.yaml
// are silently skipped and do not cause errors or phantom tools.
func TestRegistryLoadSkipsMissingYaml(t *testing.T) {
	dir := t.TempDir()

	// Create a directory with no tool.yaml inside.
	emptyDir := filepath.Join(dir, "not_a_tool")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	r := NewRegistry(dir, nil)
	if err := r.LoadCustomTools(); err != nil {
		t.Fatalf("LoadCustomTools returned unexpected error: %v", err)
	}

	// Only the 24 built-ins should be present; no extra tool from the empty dir.
	if got := len(r.List()); got != 24 {
		t.Errorf("List(): got %d tools, want 24 (only built-ins)", got)
	}
}
