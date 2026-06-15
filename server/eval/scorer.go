package eval

import "strings"

func JaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, s := range a {
		setA[strings.ToLower(s)] = true
	}
	setB := make(map[string]bool, len(b))
	for _, s := range b {
		setB[strings.ToLower(s)] = true
	}
	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func Precision(returned, expected []string) float64 {
	if len(returned) == 0 {
		return 0
	}
	exp := make(map[string]bool, len(expected))
	for _, s := range expected {
		exp[s] = true
	}
	hits := 0
	for _, s := range returned {
		if exp[s] {
			hits++
		}
	}
	return float64(hits) / float64(len(returned))
}

func Recall(returned, expected []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	ret := make(map[string]bool, len(returned))
	for _, s := range returned {
		ret[s] = true
	}
	hits := 0
	for _, s := range expected {
		if ret[s] {
			hits++
		}
	}
	return float64(hits) / float64(len(expected))
}

func MRR(ranked, expected []string) float64 {
	exp := make(map[string]bool, len(expected))
	for _, s := range expected {
		exp[s] = true
	}
	for i, s := range ranked {
		if exp[s] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

func ScoreEnrichment(c EnrichmentCase, nodeType string, tags []string, summary string) map[string]float64 {
	scores := make(map[string]float64)
	if nodeType == c.Expected.NodeType {
		scores["type_accuracy"] = 1.0
	}
	scores["tag_overlap"] = JaccardSimilarity(tags, c.Expected.Tags)
	lower := strings.ToLower(summary)
	quality := 1.0
	for _, kw := range c.Expected.SummaryMustContain {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			quality = 0
			break
		}
	}
	for _, kw := range c.Expected.SummaryMustNotContain {
		if strings.Contains(lower, strings.ToLower(kw)) {
			quality = 0
			break
		}
	}
	scores["summary_quality"] = quality
	return scores
}

func ScoreRetrieval(c RetrievalCase, returnedIDs []string) map[string]float64 {
	scores := make(map[string]float64)
	scores["precision"] = Precision(returnedIDs, c.ExpectedIDs)
	scores["recall"] = Recall(returnedIDs, c.ExpectedIDs)
	scores["mrr"] = MRR(returnedIDs, c.ExpectedIDs)
	retSet := make(map[string]bool, len(returnedIDs))
	for _, id := range returnedIDs {
		retSet[id] = true
	}
	exclusion := 1.0
	for _, id := range c.ExpectedNotIDs {
		if retSet[id] {
			exclusion = 0
			break
		}
	}
	scores["exclusion"] = exclusion
	if len(returnedIDs) == 0 {
		scores["zero_retrieval"] = 1
	} else {
		scores["zero_retrieval"] = 0
	}
	expSet := make(map[string]bool, len(c.ExpectedIDs))
	for _, id := range c.ExpectedIDs {
		expSet[id] = true
	}
	scores["expected_rank"] = 0
	for i, id := range returnedIDs {
		if expSet[id] {
			scores["expected_rank"] = float64(i + 1)
			break
		}
	}
	return scores
}

func ScoreReflection(c ReflectionCase, signalType string, confidence float64) map[string]float64 {
	scores := make(map[string]float64)
	if c.ExpectedSignal == "" {
		if signalType == "" {
			scores["signal_accuracy"] = 1.0
			scores["false_positive"] = 0.0
		} else {
			scores["signal_accuracy"] = 0.0
			scores["false_positive"] = 1.0
		}
		return scores
	}
	if signalType == c.ExpectedSignal {
		scores["signal_accuracy"] = 1.0
	}
	if confidence >= c.MinConfidence {
		scores["confidence_met"] = 1.0
	}
	scores["false_positive"] = 0.0
	return scores
}
