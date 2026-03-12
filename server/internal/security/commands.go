package security

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultDangerousCommands lists binaries that can exfiltrate data or
// expose secrets. Matched as whole words at command boundaries.
var DefaultDangerousCommands = []string{
	// Network exfiltration
	"curl", "wget", "nc", "ncat", "netcat", "socat",
	"telnet", "ssh", "scp", "sftp", "rsync",
	"python", "python3", "ruby", "perl", "node",
	"nslookup", "dig", "host",
	// Environment dumping
	"env", "printenv", "export",
}

// NetworkCommands identifies the subset of dangerous commands that perform
// network access and can be selectively unblocked via a domain allowlist.
var NetworkCommands = map[string]bool{
	"curl": true, "wget": true, "nc": true, "ncat": true, "netcat": true, "socat": true,
	"telnet": true, "ssh": true, "scp": true, "sftp": true, "rsync": true,
	"nslookup": true, "dig": true, "host": true,
}

var userHostRe = regexp.MustCompile(`\b\w+@([\w.\-]+)`)

// ExtractHosts pulls hostnames from a command string by looking for
// URLs (http(s)://host/...) and user@host patterns. Returns nil if no
// hosts can be identified.
//
// URLs are extracted with a regex scan over the full command string so
// that hosts inside shell variable assignments, heredocs, and quoted
// arguments are all discovered.
func ExtractHosts(cmdStr string) []string {
	var hosts []string
	seen := make(map[string]bool)

	// Regex scan: finds URLs anywhere in the command, including inside
	// variable assignments like ICS_URL='https://example.com/path'.
	for _, raw := range urlInTextRe.FindAllString(cmdStr, -1) {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			h := u.Hostname()
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}

	// Extract hosts from user@host patterns (ssh, scp, rsync).
	for _, m := range userHostRe.FindAllStringSubmatch(cmdStr, -1) {
		h := m[1]
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}

	return hosts
}

// urlInTextRe matches http and https URLs embedded in arbitrary text
// (markdown links, code blocks, prose). It stops at whitespace and common
// delimiters like ), >, ], ", ', and backtick.
var urlInTextRe = regexp.MustCompile(`https?://[^\s)>\]"'` + "`" + `]+`)

// ExtractDomainsFromText finds all http/https URLs in arbitrary text and
// returns deduplicated hostnames. Useful for pulling domain names out of
// skill content that may contain markdown, code fences, etc.
func ExtractDomainsFromText(text string) []string {
	seen := make(map[string]bool)
	var domains []string
	for _, raw := range urlInTextRe.FindAllString(text, -1) {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		h := u.Hostname()
		if !seen[h] {
			seen[h] = true
			domains = append(domains, h)
		}
	}
	return domains
}

// MatchesDomain checks if a host matches a domain pattern. Supports exact
// match ("api.weather.com") and wildcard prefix ("*.github.com" matches
// "api.github.com" but not "github.com" itself).
func MatchesDomain(host string, pattern string) bool {
	host = strings.ToLower(host)
	pattern = strings.ToLower(pattern)
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".github.com"
		return strings.HasSuffix(host, suffix) && host != suffix[1:]
	}
	return host == pattern
}

// AllHostsAllowed returns true if every host extracted from cmdStr matches
// at least one pattern in allowedDomains. Returns false if no hosts could
// be extracted (fail-closed).
func AllHostsAllowed(cmdStr string, allowedDomains []string) bool {
	hosts := ExtractHosts(cmdStr)
	if len(hosts) == 0 {
		return false
	}
	for _, h := range hosts {
		allowed := false
		for _, pattern := range allowedDomains {
			if MatchesDomain(h, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

// shellMetachars contains characters that separate tokens in shell commands.
const shellMetachars = " \t\n|&;()<>`\"'$"

// ContainsDangerousCommand checks whether cmdStr invokes any dangerous binary.
// Uses word-boundary matching: "curl" matches "curl http://..." and
// "/usr/bin/curl" but not "curling" or "my_curl_wrapper".
// Returns the matched command name or empty string.
func ContainsDangerousCommand(cmdStr string, commands []string, allowedDomains []string) (bool, string) {
	tokens := Tokenize(cmdStr)
	for _, tok := range tokens {
		base := filepath.Base(tok)
		for _, cmd := range commands {
			if base == cmd {
				if NetworkCommands[cmd] && len(allowedDomains) > 0 {
					if AllHostsAllowed(cmdStr, allowedDomains) {
						continue
					}
				}
				return true, cmd
			}
		}
	}
	return false, ""
}

// Tokenize splits a shell command string into tokens by splitting on
// metacharacters, then stripping shell syntax remnants like $( and `.
func Tokenize(s string) []string {
	var tokens []string
	f := func(r rune) bool {
		return strings.ContainsRune(shellMetachars, r)
	}
	for _, part := range strings.FieldsFunc(s, f) {
		// Strip leading shell syntax: $( or leading backticks handled by split.
		part = strings.TrimLeft(part, "$()`")
		if part != "" {
			tokens = append(tokens, part)
		}
	}
	return tokens
}
