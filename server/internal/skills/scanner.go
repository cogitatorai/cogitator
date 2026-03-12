package skills

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity indicates how dangerous a finding is.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
)

// Finding represents a single suspicious pattern detected in skill content.
type Finding struct {
	Pattern     string   `json:"pattern"`
	Severity    Severity `json:"severity"`
	Description string   `json:"description"`
	Line        int      `json:"line"`
	Snippet     string   `json:"snippet"`
}

// ScanResult holds the outcome of scanning a skill's content.
type ScanResult struct {
	Findings []Finding `json:"findings,omitempty"`
	Blocked  bool      `json:"blocked"`
	Size     int       `json:"size"`
}

type rule struct {
	re          *regexp.Regexp
	severity    Severity
	description string
}

var rules []rule

func init() {
	high := []struct {
		pattern     string
		description string
	}{
		// Pipe-to-shell: curl/wget output piped to an interpreter.
		{`(?i)(curl|wget)\s+[^\n|]*\|\s*(sh|bash|zsh|python|python3|perl|ruby|node)`, "pipe-to-shell: command output piped to interpreter"},
		// Base64 decode piped to interpreter or shell.
		{`(?i)base64\s+(-d|--decode)[^\n]*\|\s*(sh|bash|python|python3|perl|ruby|node)`, "base64 decode piped to interpreter"},
		// Bare IP addresses in URLs (legitimate dependencies use DNS).
		{`https?://\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}[:/]`, "bare IP address in URL"},
		// Reading sensitive credential paths.
		{`~/\.(ssh|aws|gnupg|config/gcloud|kube|docker)(/|\s|"|')`, "access to sensitive credential path"},
		// SSL verification bypass.
		{`(?i)(curl|wget)\s[^\n]*(--insecure|-k\s)`, "SSL verification disabled"},
		// Hex-encoded payloads piped to interpreter.
		{`(?i)(\\x[0-9a-f]{2}){4,}[^\n]*\|\s*(sh|bash|python|python3|perl)`, "hex-encoded payload piped to interpreter"},
		// eval/exec with string argument (common payload execution pattern).
		{`(?i)\b(eval|exec)\s*\(\s*["']`, "eval/exec with string argument"},
	}

	medium := []struct {
		pattern     string
		description string
	}{
		// Non-HTTPS URL in curl/wget.
		{`(?i)(curl|wget)\s+[^\n]*http://\S+`, "non-HTTPS URL in download command"},
		// chmod +x followed by execution.
		{`(?i)chmod\s+\+x\s+\S+`, "making file executable"},
		// Writing to shell dotfiles (persistence).
		{`(?i)>>\s*~/?\.(bashrc|zshrc|profile|bash_profile)`, "appending to shell dotfile"},
		// Background persistence.
		{`(?i)\b(nohup|disown)\b`, "background process persistence"},
		// Writing to /tmp (common staging area for payloads).
		{`(?i)/tmp/\.\w+`, "hidden file in /tmp"},
	}

	for _, r := range high {
		rules = append(rules, rule{
			re:          regexp.MustCompile(r.pattern),
			severity:    SeverityHigh,
			description: r.description,
		})
	}
	for _, r := range medium {
		rules = append(rules, rule{
			re:          regexp.MustCompile(r.pattern),
			severity:    SeverityMedium,
			description: r.description,
		})
	}
}

// ScanContent checks skill content for suspicious patterns and returns
// a ScanResult. This is a pure function with no side effects.
func ScanContent(content []byte) ScanResult {
	result := ScanResult{Size: len(content)}
	lines := strings.Split(string(content), "\n")

	for _, r := range rules {
		for i, line := range lines {
			if r.re.MatchString(line) {
				snippet := line
				if len(snippet) > 120 {
					snippet = snippet[:120] + "..."
				}
				result.Findings = append(result.Findings, Finding{
					Pattern:     r.re.String(),
					Severity:    r.severity,
					Description: r.description,
					Line:        i + 1,
					Snippet:     strings.TrimSpace(snippet),
				})
				if r.severity == SeverityHigh {
					result.Blocked = true
				}
			}
		}
	}

	return result
}

// FormatFindings returns a human-readable summary of scan findings.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return "No issues found."
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "[%s] Line %d: %s\n  > %s\n", strings.ToUpper(string(f.Severity)), f.Line, f.Description, f.Snippet)
	}
	return b.String()
}
