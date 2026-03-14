package browser

import (
	"strings"
	"testing"
)

func TestFormatAccessibilityTree(t *testing.T) {
	nodes := []AXNode{
		{NodeID: "1", Role: AXValue{Value: "RootWebArea"}, Name: AXValue{Value: "Example"}},
		{NodeID: "2", ParentID: "1", Role: AXValue{Value: "navigation"}, Name: AXValue{Value: "Main"}},
		{NodeID: "3", ParentID: "2", Role: AXValue{Value: "link"}, Name: AXValue{Value: "Home"}},
		{NodeID: "4", ParentID: "2", Role: AXValue{Value: "link"}, Name: AXValue{Value: "About"}},
		{NodeID: "5", ParentID: "1", Role: AXValue{Value: "heading"}, Name: AXValue{Value: "Welcome"}},
		{NodeID: "6", ParentID: "1", Role: AXValue{Value: "textbox"}, Name: AXValue{Value: "Search"}, Value: &AXValue{Value: "hello"}},
	}

	result := FormatAccessibilityTree(nodes)

	// Should contain structured output
	if !strings.Contains(result, "[RootWebArea] \"Example\"") {
		t.Errorf("missing root node, got:\n%s", result)
	}
	if !strings.Contains(result, "[navigation] \"Main\"") {
		t.Errorf("missing navigation, got:\n%s", result)
	}
	if !strings.Contains(result, "[link] \"Home\"") {
		t.Errorf("missing link, got:\n%s", result)
	}
	if !strings.Contains(result, "[textbox] \"Search\" = \"hello\"") {
		t.Errorf("missing textbox with value, got:\n%s", result)
	}

	// Check indentation (navigation is child of root, so indented)
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[navigation]") {
			if !strings.HasPrefix(line, "  ") {
				t.Errorf("navigation should be indented, got: %q", line)
			}
		}
		if strings.Contains(line, "[link]") {
			if !strings.HasPrefix(line, "    ") {
				t.Errorf("link should be double-indented, got: %q", line)
			}
		}
	}
}

func TestFormatAccessibilityTreeFiltersNoise(t *testing.T) {
	nodes := []AXNode{
		{NodeID: "1", Role: AXValue{Value: "RootWebArea"}, Name: AXValue{Value: "Test"}},
		{NodeID: "2", ParentID: "1", Role: AXValue{Value: "none"}, Name: AXValue{Value: ""}},
		{NodeID: "3", ParentID: "1", Role: AXValue{Value: "generic"}, Name: AXValue{Value: ""}},
		{NodeID: "4", ParentID: "1", Role: AXValue{Value: "InlineTextBox"}, Name: AXValue{Value: "text"}},
		{NodeID: "5", ParentID: "1", Role: AXValue{Value: "button"}, Name: AXValue{Value: "Click me"}},
	}

	result := FormatAccessibilityTree(nodes)

	if strings.Contains(result, "none") {
		t.Errorf("should filter 'none' role, got:\n%s", result)
	}
	if strings.Contains(result, "generic") {
		t.Errorf("should filter 'generic' role, got:\n%s", result)
	}
	if strings.Contains(result, "InlineTextBox") {
		t.Errorf("should filter 'InlineTextBox' role, got:\n%s", result)
	}
	if !strings.Contains(result, "[button] \"Click me\"") {
		t.Errorf("should include button, got:\n%s", result)
	}
}

func TestFormatAccessibilityTreeEmpty(t *testing.T) {
	result := FormatAccessibilityTree(nil)
	if result != "" {
		t.Errorf("expected empty string for nil nodes, got: %q", result)
	}
}
