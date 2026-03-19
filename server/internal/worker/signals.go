package worker

import (
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/session"
)

var correctionPatterns = []string{
	"that's wrong", "no, i meant", "not what i", "don't do that", "stop doing",
	"i didn't ask for", "this isn't right", "please don't", "that's incorrect",
	"you got it wrong",
}

var refinementPatterns = []string{
	"actually", "i meant", "more like", "instead of", "let me rephrase",
	"what i really want", "to clarify", "not exactly", "closer to",
}

var acknowledgmentPatterns = []string{
	"perfect", "exactly", "great", "that's right", "well done", "love it",
	"this is great", "spot on", "nailed it", "that's what i wanted",
}

type detectedSignal struct {
	Type         string
	Summary      string
	Confidence   float64
	MessageIndex int
}

// detectSignals scans conversation messages for behavioral signals using
// pattern matching. Returns signals for user messages that follow assistant messages.
func detectSignals(messages []session.Message) []detectedSignal {
	var signals []detectedSignal

	for i := 1; i < len(messages); i++ {
		if messages[i].Role != "user" || messages[i-1].Role != "assistant" {
			continue
		}

		text := strings.ToLower(messages[i].Content)

		if sig, ok := matchPatterns(text, "correction", correctionPatterns, i); ok {
			signals = append(signals, sig)
		} else if sig, ok := matchPatterns(text, "refinement", refinementPatterns, i); ok {
			signals = append(signals, sig)
		} else if sig, ok := matchPatterns(text, "acknowledgment", acknowledgmentPatterns, i); ok {
			signals = append(signals, sig)
		}
	}

	return signals
}

func matchPatterns(text, signalType string, patterns []string, msgIndex int) (detectedSignal, bool) {
	matches := 0
	for _, p := range patterns {
		if strings.Contains(text, p) {
			matches++
		}
	}
	if matches == 0 {
		return detectedSignal{}, false
	}

	conf := 0.7
	if matches >= 2 {
		conf = 0.85
	}
	if matches >= 3 {
		conf = 0.95
	}

	return detectedSignal{
		Type:         signalType,
		Confidence:   conf,
		MessageIndex: msgIndex,
	}, true
}

// isLikelyNonEnglish returns true if the text appears to be non-English based
// on the ratio of non-ASCII runes.
func isLikelyNonEnglish(text string) bool {
	if len(text) == 0 {
		return false
	}
	nonASCII := 0
	for _, r := range text {
		if r > 127 {
			nonASCII++
		}
	}
	return float64(nonASCII)/float64(len([]rune(text))) > 0.3
}
