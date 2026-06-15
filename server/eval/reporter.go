package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// WriteTable writes a human-readable report to w showing per-case detail.
func WriteTable(w io.Writer, r *Report) {
	fmt.Fprintf(w, "Cogitator Eval Report\n")
	fmt.Fprintf(w, "Model: %s/%s    Cases: %d    Total: %.2f\n",
		r.Provider, r.Model, totalCases(r), r.Total)
	fmt.Fprintln(w)

	for _, s := range r.Stages {
		passed := 0
		failed := 0
		errored := 0
		for _, cr := range s.Results {
			if cr.Error != "" {
				errored++
			} else if cr.Pass {
				passed++
			} else {
				failed++
			}
		}

		fmt.Fprintf(w, "%s (%d cases: %d passed, %d failed, %d errors, %d cached)\n",
			strings.ToUpper(s.Stage), s.Cases, passed, failed, errored, s.Cached)

		// Per-case detail.
		for _, cr := range s.Results {
			if cr.Error != "" {
				fmt.Fprintf(w, "  [ERR]  %-30s %s\n", cr.ID, cr.Error)
				continue
			}
			status := "PASS"
			if !cr.Pass {
				status = "FAIL"
			}
			scores := formatScores(cr.Scores)
			fmt.Fprintf(w, "  [%s] %-30s %s\n", status, cr.ID, scores)
		}

		// Stage averages.
		if len(s.Metrics) > 0 {
			fmt.Fprintf(w, "  Averages: %s\n", formatScores(s.Metrics))
		}
		fmt.Fprintln(w)
	}
}

// WriteJSON writes the report as JSON to w.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteComparison writes a side-by-side comparison of multiple reports.
func WriteComparison(w io.Writer, reports []*Report) {
	if len(reports) == 0 {
		return
	}

	allKeys := make(map[string]bool)
	for _, r := range reports {
		for _, s := range r.Stages {
			for k := range s.Metrics {
				allKeys[s.Stage+"/"+k] = true
			}
		}
	}
	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Fprintf(w, "%-30s", "")
	for _, r := range reports {
		label := r.Provider + "/" + r.Model
		if len(label) > 18 {
			label = label[:18]
		}
		fmt.Fprintf(w, "%-20s", label)
	}
	fmt.Fprintln(w)

	for _, key := range keys {
		fmt.Fprintf(w, "%-30s", key)
		parts := strings.SplitN(key, "/", 2)
		for _, r := range reports {
			val := findMetric(r, parts[0], parts[1])
			fmt.Fprintf(w, "%-20s", fmt.Sprintf("%.2f", val))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "%-30s", "TOTAL")
	for _, r := range reports {
		fmt.Fprintf(w, "%-20s", fmt.Sprintf("%.2f", r.Total))
	}
	fmt.Fprintln(w)
}

func formatScores(scores map[string]float64) string {
	keys := sortedKeys(scores)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.2f", k, scores[k]))
	}
	return strings.Join(parts, "  ")
}

func totalCases(r *Report) int {
	n := 0
	for _, s := range r.Stages {
		n += s.Cases
	}
	return n
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func findMetric(r *Report, stage, metric string) float64 {
	for _, s := range r.Stages {
		if s.Stage == stage {
			return s.Metrics[metric]
		}
	}
	return 0
}
