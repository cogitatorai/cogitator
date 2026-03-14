package browser

import (
	"testing"
)

func TestToolDefs(t *testing.T) {
	defs := ToolDefs()
	if len(defs) != 15 {
		t.Fatalf("expected 15 tool definitions, got %d", len(defs))
	}
	names := make(map[string]bool)
	for _, d := range defs {
		if names[d.Name] {
			t.Errorf("duplicate tool name: %s", d.Name)
		}
		names[d.Name] = true
		if d.Description == "" {
			t.Errorf("tool %s has empty description", d.Name)
		}
		if d.Parameters == nil {
			t.Errorf("tool %s has nil parameters", d.Name)
		}
	}
	// Verify specific tools exist
	expectedTools := []string{
		"browser_list", "browser_open", "browser_close", "browser_navigate",
		"browser_snapshot", "browser_html", "browser_screenshot",
		"browser_click", "browser_click_xy", "browser_type", "browser_key",
		"browser_eval", "browser_scroll", "browser_network", "browser_load_all",
	}
	for _, name := range expectedTools {
		if !names[name] {
			t.Errorf("missing tool definition: %s", name)
		}
	}
}
