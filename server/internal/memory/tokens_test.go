package memory

import "testing"

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"short", "hello world", 2},
		{"exact", "abcd", 1},
		{"longer", "abcdefghi", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateTokens(tt.content); got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestEstimateTokensFromLength(t *testing.T) {
	tests := []struct {
		name   string
		length int
		want   int
	}{
		{"zero", 0, headerTokenOverhead},
		{"400 bytes", 400, 100 + headerTokenOverhead},
		{"negative treated as zero", -1, headerTokenOverhead},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateTokensFromLength(tt.length); got != tt.want {
				t.Errorf("estimateTokensFromLength(%d) = %d, want %d", tt.length, got, tt.want)
			}
		})
	}
}
