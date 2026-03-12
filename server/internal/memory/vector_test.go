package memory

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := CosineSimilarity(a, a)
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("identical vectors: got %f, want 1.0", got)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	got := CosineSimilarity(a, b)
	if math.Abs(got) > 1e-6 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{-1, 0, 0}
	got := CosineSimilarity(a, b)
	if math.Abs(got-(-1.0)) > 1e-6 {
		t.Errorf("opposite vectors: got %f, want -1.0", got)
	}
}

func TestCosineSimilaritySimilar(t *testing.T) {
	// (1,1,0) vs (1,0,0): cos(45 deg) = 1/sqrt(2) ~= 0.7071
	a := []float32{1, 1, 0}
	b := []float32{1, 0, 0}
	got := CosineSimilarity(a, b)
	want := 1.0 / math.Sqrt2
	if math.Abs(got-want) > 1e-5 {
		t.Errorf("similar vectors: got %f, want %f", got, want)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("zero vector: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityLengthMismatch(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("length mismatch: got %f, want 0.0", got)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	got := CosineSimilarity([]float32{}, []float32{})
	if got != 0 {
		t.Errorf("empty vectors: got %f, want 0.0", got)
	}
}

func TestRecencyBoostZeroDays(t *testing.T) {
	// 0 days: score * (1 + alpha * exp(0)) = score * (1 + alpha)
	similarity := 0.8
	alpha := 0.5
	lambda := 0.01
	got := recencyBoost(similarity, 0, alpha, lambda)
	want := similarity * (1 + alpha)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("0 days: got %f, want %f", got, want)
	}
}

func TestRecencyBoostLargeDays(t *testing.T) {
	// Very large days: exp(-lambda * days) -> 0, so score approaches score * 1.0
	similarity := 0.8
	alpha := 0.5
	lambda := 0.01
	got := recencyBoost(similarity, 10000, alpha, lambda)
	// Should be within 0.001 of the base similarity.
	if math.Abs(got-similarity) > 0.001 {
		t.Errorf("large days: got %f, want ~%f", got, similarity)
	}
}

func TestRecencyBoostMonotonicallyDecreasing(t *testing.T) {
	// As days increase, boost decreases monotonically.
	alpha := 0.5
	lambda := 0.01
	sim := 0.9
	prev := recencyBoost(sim, 0, alpha, lambda)
	for _, days := range []float64{1, 10, 50, 100, 500} {
		cur := recencyBoost(sim, days, alpha, lambda)
		if cur >= prev {
			t.Errorf("boost at %v days (%f) not less than at fewer days (%f)", days, cur, prev)
		}
		prev = cur
	}
}
