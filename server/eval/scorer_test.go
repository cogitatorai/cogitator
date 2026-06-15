package eval

import "testing"

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		min  float64
		max  float64
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, 1.0, 1.0},
		{"half overlap", []string{"a", "b"}, []string{"b", "c"}, 0.33, 0.34},
		{"no overlap", []string{"a"}, []string{"b"}, 0.0, 0.0},
		{"empty", nil, nil, 0.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JaccardSimilarity(tt.a, tt.b)
			if got < tt.min || got > tt.max {
				t.Errorf("Jaccard(%v, %v) = %f, want [%f, %f]", tt.a, tt.b, got, tt.min, tt.max)
			}
		})
	}
}

func TestPrecisionRecall(t *testing.T) {
	expected := []string{"a", "b", "c"}
	got := []string{"a", "b", "d"}
	p := Precision(got, expected)
	if p < 0.66 || p > 0.67 {
		t.Errorf("Precision = %f, want ~0.67", p)
	}
	r := Recall(got, expected)
	if r < 0.66 || r > 0.67 {
		t.Errorf("Recall = %f, want ~0.67", r)
	}
}

func TestMRR(t *testing.T) {
	ranked := []string{"x", "a", "b"}
	expected := []string{"a", "b"}
	mrr := MRR(ranked, expected)
	if mrr != 0.5 {
		t.Errorf("MRR = %f, want 0.5", mrr)
	}
}

func TestScoreEnrichment(t *testing.T) {
	c := EnrichmentCase{
		Expected: EnrichmentExpected{
			NodeType:           "preference",
			Tags:               []string{"coffee", "food"},
			TagMinOverlap:      0.5,
			SummaryMustContain: []string{"dark roast"},
		},
	}
	scores := ScoreEnrichment(c, "preference", []string{"coffee", "drink"}, "the user prefers dark roast coffee")
	if scores["type_accuracy"] != 1.0 {
		t.Errorf("type_accuracy = %f, want 1.0", scores["type_accuracy"])
	}
	if scores["summary_quality"] != 1.0 {
		t.Errorf("summary_quality = %f, want 1.0", scores["summary_quality"])
	}
}

func TestScoreRetrievalZeroAndRank(t *testing.T) {
	c := RetrievalCase{ExpectedIDs: []string{"a", "b"}}

	s := ScoreRetrieval(c, nil)
	if s["zero_retrieval"] != 1 {
		t.Errorf("zero_retrieval = %v, want 1", s["zero_retrieval"])
	}
	if s["expected_rank"] != 0 {
		t.Errorf("expected_rank = %v, want 0 (absent)", s["expected_rank"])
	}

	s2 := ScoreRetrieval(c, []string{"x", "b", "a"})
	if s2["zero_retrieval"] != 0 {
		t.Errorf("zero_retrieval = %v, want 0", s2["zero_retrieval"])
	}
	if s2["expected_rank"] != 2 {
		t.Errorf("expected_rank = %v, want 2", s2["expected_rank"])
	}
}
