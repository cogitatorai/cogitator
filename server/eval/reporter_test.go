package eval

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTable(t *testing.T) {
	r := &Report{
		Provider: "openai",
		Model:    "gpt-4o",
		Total:    0.85,
		Stages: []StageResult{{
			Stage: "enrichment", Cases: 3, Cached: 1,
			Metrics: map[string]float64{"type_accuracy": 0.67, "tag_overlap": 0.5},
			Results: []CaseResult{
				{ID: "case_1", Stage: "enrichment", Pass: true, Scores: map[string]float64{"type_accuracy": 1.0, "tag_overlap": 0.8}},
				{ID: "case_2", Stage: "enrichment", Pass: false, Scores: map[string]float64{"type_accuracy": 0.0, "tag_overlap": 0.2}},
				{ID: "case_3", Stage: "enrichment", Error: "parse error: unexpected EOF"},
			},
		}},
	}
	var buf bytes.Buffer
	WriteTable(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "openai/gpt-4o") {
		t.Errorf("missing model header")
	}
	if !strings.Contains(out, "ENRICHMENT") {
		t.Errorf("missing stage header")
	}
	if !strings.Contains(out, "1 passed") {
		t.Errorf("missing pass count")
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("missing fail count")
	}
	if !strings.Contains(out, "1 errors") {
		t.Errorf("missing error count")
	}
	if !strings.Contains(out, "[PASS]") {
		t.Errorf("missing PASS marker")
	}
	if !strings.Contains(out, "[FAIL]") {
		t.Errorf("missing FAIL marker")
	}
	if !strings.Contains(out, "[ERR]") {
		t.Errorf("missing ERR marker")
	}
	if !strings.Contains(out, "parse error") {
		t.Errorf("missing error message")
	}
}

func TestWriteComparison(t *testing.T) {
	r1 := &Report{Provider: "openai", Model: "gpt-4o", Total: 0.85,
		Stages: []StageResult{{Stage: "enrichment", Metrics: map[string]float64{"type_accuracy": 0.9}}}}
	r2 := &Report{Provider: "anthropic", Model: "sonnet", Total: 0.90,
		Stages: []StageResult{{Stage: "enrichment", Metrics: map[string]float64{"type_accuracy": 1.0}}}}

	var buf bytes.Buffer
	WriteComparison(&buf, []*Report{r1, r2})
	out := buf.String()
	if !strings.Contains(out, "openai/gpt-4o") {
		t.Errorf("missing first model")
	}
	if !strings.Contains(out, "anthropic/sonnet") {
		t.Errorf("missing second model")
	}
	if !strings.Contains(out, "TOTAL") {
		t.Errorf("missing TOTAL row")
	}
}
