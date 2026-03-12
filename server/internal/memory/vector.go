package memory

import "math"

// CosineSimilarity computes the cosine of the angle between two vectors.
// Returns 0 if either vector has zero magnitude or the lengths differ.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// recencyBoost applies exponential decay boosting to a similarity score.
// The formula is: score * (1 + alpha * exp(-lambda * daysSinceUpdate)).
// Recent nodes (low daysSinceUpdate) receive a larger boost.
func recencyBoost(similarity float64, daysSinceUpdate float64, alpha, lambda float64) float64 {
	return similarity * (1 + alpha*math.Exp(-lambda*daysSinceUpdate))
}
