package browser

import (
	"strings"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	targets := []TargetInfo{
		{ID: "ABC123DEF456", Type: "page", Title: "Example", URL: "https://example.com"},
		{ID: "XYZ789GHI012", Type: "page", Title: "Test", URL: "https://test.com"},
	}

	// Exact prefix match
	id, err := resolveTarget("ABC", targets)
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if id != "ABC123DEF456" {
		t.Errorf("expected ABC123DEF456, got %s", id)
	}

	// Case-insensitive
	id, err = resolveTarget("abc", targets)
	if err != nil {
		t.Fatalf("resolveTarget case-insensitive: %v", err)
	}
	if id != "ABC123DEF456" {
		t.Errorf("expected ABC123DEF456, got %s", id)
	}

	// Full ID match
	id, err = resolveTarget("XYZ789GHI012", targets)
	if err != nil {
		t.Fatalf("resolveTarget full: %v", err)
	}
	if id != "XYZ789GHI012" {
		t.Errorf("expected XYZ789GHI012, got %s", id)
	}

	// No match
	_, err = resolveTarget("NOMATCH", targets)
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestResolveTargetAmbiguous(t *testing.T) {
	targets := []TargetInfo{
		{ID: "ABC123", Type: "page", Title: "One", URL: "https://one.com"},
		{ID: "ABC456", Type: "page", Title: "Two", URL: "https://two.com"},
	}

	_, err := resolveTarget("ABC", targets)
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected 'ambiguous' in error, got: %s", err)
	}
}

func TestParamString(t *testing.T) {
	params := map[string]any{
		"url":    "https://example.com",
		"number": 42.0,
	}
	if got := paramString(params, "url"); got != "https://example.com" {
		t.Errorf("expected url, got %q", got)
	}
	if got := paramString(params, "missing"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParamFloat(t *testing.T) {
	params := map[string]any{
		"x": 100.5,
	}
	if got := paramFloat(params, "x", 0); got != 100.5 {
		t.Errorf("expected 100.5, got %f", got)
	}
	if got := paramFloat(params, "missing", 42.0); got != 42.0 {
		t.Errorf("expected 42.0, got %f", got)
	}
}
