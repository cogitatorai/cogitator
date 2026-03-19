package memory

import (
	"strings"
)

const (
	maxTriggers = 100
	maxTags     = 10
)

// CleanTriggers deduplicates, removes empty/substring entries, and caps at maxTriggers.
func CleanTriggers(triggers []string) []string {
	if len(triggers) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(triggers))
	var cleaned []string
	for _, t := range triggers {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		cleaned = append(cleaned, t)
	}

	var result []string
	for _, t := range cleaned {
		isSubstring := false
		for _, other := range cleaned {
			if t != other && strings.Contains(other, t) {
				isSubstring = true
				break
			}
		}
		if !isSubstring {
			result = append(result, t)
		}
	}

	if len(result) > maxTriggers {
		result = result[:maxTriggers]
	}
	return result
}

// CleanTags deduplicates, removes empty entries, and caps at maxTags.
func CleanTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(tags))
	var result []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		result = append(result, t)
	}

	if len(result) > maxTags {
		result = result[:maxTags]
	}
	return result
}

// TitleJaccard computes the Jaccard similarity of word sets from two titles.
// Returns 0.0 for empty inputs.
func TitleJaccard(a, b string) float64 {
	wordsA := titleWords(a)
	wordsB := titleWords(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}
	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// ValidatedEnrichment holds the server-validated enrichment result.
type ValidatedEnrichment struct {
	NodeType NodeType
	Summary  string
	Tags     []string
	Triggers []string
}

// preferenceKeywords are subjective language indicators that bias toward NodePreference.
var preferenceKeywords = []string{"likes", "prefers", "enjoys", "hates", "dislikes", "loves", "favorite"}

// ValidateEnrichmentResult cleans and validates raw LLM enrichment output.
// userNames is a list of person names to strip from the summary.
// content is the node's raw content, used for preference keyword detection.
func ValidateEnrichmentResult(nodeType string, summary string, tags, triggers []string, userNames []string, content string) ValidatedEnrichment {
	// Validate node type.
	nt := NodeType(nodeType)
	switch nt {
	case NodeFact, NodePreference, NodePattern:
		// valid
	default:
		nt = NodeFact
	}

	// Preference keyword bias: if content contains subjective language, override to preference.
	if nt == NodeFact && content != "" {
		lower := strings.ToLower(content)
		for _, kw := range preferenceKeywords {
			if strings.Contains(lower, kw) {
				nt = NodePreference
				break
			}
		}
	}

	// Strip person names from summary.
	for _, name := range userNames {
		summary = strings.ReplaceAll(summary, name, "the user")
	}
	if len(summary) > 200 {
		summary = summary[:200]
	}

	return ValidatedEnrichment{
		NodeType: nt,
		Summary:  summary,
		Tags:     CleanTags(tags),
		Triggers: CleanTriggers(triggers),
	}
}

// stopWords are common English words to exclude from Jaccard comparison.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "can": true, "shall": true,
	"to": true, "of": true, "in": true, "for": true, "on": true,
	"with": true, "at": true, "by": true, "from": true, "as": true,
	"into": true, "about": true, "that": true, "this": true, "it": true,
	"and": true, "or": true, "but": true, "not": true, "no": true,
	"user": true, "the user": true,
	"likes": true, "prefers": true, "enjoys": true, "wants": true, "uses": true,
}

func titleWords(s string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if !stopWords[w] && len(w) > 1 {
			words[w] = true
		}
	}
	return words
}
