package memory

import (
	"strings"
	"testing"
)

func TestCleanTriggers(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  int
	}{
		{"dedup", []string{"hiking", "Hiking", "HIKING"}, 1},
		{"empty removal", []string{"hiking", "", "  ", "camping"}, 2},
		{"substring removal", []string{"hiking", "hiking destinations", "camping"}, 2},
		{"cap at 100", make([]string, 150), 0},
		{"nil input", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanTriggers(tt.input)
			if len(got) != tt.want {
				t.Errorf("CleanTriggers() returned %d items, want %d: %v", len(got), tt.want, got)
			}
		})
	}
}

func TestCleanTags(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  int
	}{
		{"dedup", []string{"coffee", "Coffee", "COFFEE"}, 1},
		{"empty removal", []string{"coffee", "", "tea"}, 2},
		{"cap at 10", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"}, 10},
		{"nil input", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanTags(tt.input)
			if len(got) != tt.want {
				t.Errorf("CleanTags() returned %d items, want %d: %v", len(got), tt.want, got)
			}
		})
	}
}

func TestTitleJaccard(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		min  float64
	}{
		{"identical", "User likes hiking", "User likes hiking", 1.0},
		{"similar", "User likes hiking", "User enjoys hiking", 0.5},
		{"different", "User likes hiking", "Prefers dark coffee", 0.0},
		{"empty", "", "", 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TitleJaccard(tt.a, tt.b)
			if got < tt.min {
				t.Errorf("TitleJaccard(%q, %q) = %f, want >= %f", tt.a, tt.b, got, tt.min)
			}
		})
	}
}

func TestValidateEnrichmentResult(t *testing.T) {
	t.Run("invalid type defaults to fact", func(t *testing.T) {
		result := ValidateEnrichmentResult("garbage", "summary", nil, nil, nil, "")
		if result.NodeType != NodeFact {
			t.Errorf("NodeType = %s, want fact", result.NodeType)
		}
	})

	t.Run("preference keyword bias", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "summary", nil, nil, nil, "The user likes hiking")
		if result.NodeType != NodePreference {
			t.Errorf("NodeType = %s, want preference (keyword bias)", result.NodeType)
		}
	})

	t.Run("summary strips person names", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "Guillaume likes coffee", nil, nil, []string{"Guillaume", "Andrei"}, "")
		if strings.Contains(result.Summary, "Guillaume") {
			t.Errorf("Summary should not contain person name: %s", result.Summary)
		}
	})

	t.Run("summary max length", func(t *testing.T) {
		long := strings.Repeat("x", 300)
		result := ValidateEnrichmentResult("fact", long, nil, nil, nil, "")
		if len(result.Summary) > 200 {
			t.Errorf("Summary length = %d, want <= 200", len(result.Summary))
		}
	})

	t.Run("tags cleaned", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "summary", []string{"Coffee", "coffee", "", "TEA"}, nil, nil, "")
		if len(result.Tags) != 2 {
			t.Errorf("Tags count = %d, want 2", len(result.Tags))
		}
	})

	t.Run("triggers cleaned", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "summary", nil, []string{"hiking", "Hiking", "hiking destinations"}, nil, "")
		if len(result.Triggers) != 1 {
			t.Errorf("Triggers count = %d, want 1: %v", len(result.Triggers), result.Triggers)
		}
	})
}
