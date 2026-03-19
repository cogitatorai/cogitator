package memory

import (
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
