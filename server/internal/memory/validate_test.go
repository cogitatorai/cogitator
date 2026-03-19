package memory

import (
	"strings"
	"testing"
)

func TestFindDuplicate(t *testing.T) {
	db := testDB(t)
	store := NewStore(db)

	id1, _ := store.CreateNode(&Node{Type: NodeFact, Title: "User likes hiking"})
	store.SaveEmbedding(id1, []float32{0.9, 0.1, 0.0}, "test")

	// Similar title and embedding: should be detected as duplicate.
	dup := FindDuplicate(store, "new-node-id", "User enjoys hiking", NodeFact, nil, []float32{0.9, 0.1, 0.0}, 0.90)
	if dup == "" {
		t.Error("expected duplicate to be found")
	}
	if dup != id1 {
		t.Errorf("expected dup=%s, got %s", id1, dup)
	}

	// Self-match excluded.
	self := FindDuplicate(store, id1, "User likes hiking", NodeFact, nil, []float32{0.9, 0.1, 0.0}, 0.90)
	if self != "" {
		t.Error("self-match should be excluded")
	}

	// Different type: not a duplicate.
	dup2 := FindDuplicate(store, "new-node-id", "User enjoys hiking", NodePreference, nil, []float32{0.9, 0.1, 0.0}, 0.90)
	if dup2 != "" {
		t.Error("different type should not match")
	}

	// Low similarity: not a duplicate.
	dup3 := FindDuplicate(store, "new-node-id", "User likes hiking", NodeFact, nil, []float32{0.0, 0.0, 1.0}, 0.90)
	if dup3 != "" {
		t.Error("low similarity should not match")
	}
}

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

	t.Run("summary strips person name at start", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "Guillaume likes coffee", nil, nil, []string{"Guillaume", "Andrei"}, "")
		if strings.HasPrefix(result.Summary, "Guillaume") {
			t.Errorf("Summary should not start with person name: %s", result.Summary)
		}
		if !strings.Contains(result.Summary, "the user") {
			t.Errorf("Summary should start with 'the user': %s", result.Summary)
		}
	})

	t.Run("summary preserves person name as value", func(t *testing.T) {
		result := ValidateEnrichmentResult("fact", "A friend named Andrei", nil, nil, []string{"Andrei"}, "")
		if !strings.Contains(result.Summary, "Andrei") {
			t.Errorf("Summary should preserve name as value: %s", result.Summary)
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
