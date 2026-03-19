package memory

// headerTokenOverhead is the estimated token cost of the formatted header
// (title, type label, etc.) added to each retrieved node in the output.
const headerTokenOverhead = 30

// estimateTokens estimates the token count of a content string using the
// conservative ratio of 4 chars per token (BPE averages ~3.5 for English).
func estimateTokens(content string) int {
	return len(content) / 4
}

// estimateTokensFromLength estimates the token cost of a node from its
// stored content_length. Adds headerTokenOverhead for the formatted header.
func estimateTokensFromLength(contentLength int) int {
	if contentLength < 0 {
		contentLength = 0
	}
	return contentLength/4 + headerTokenOverhead
}
