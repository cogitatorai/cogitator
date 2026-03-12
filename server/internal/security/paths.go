package security

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultSensitivePaths lists credential stores and system files that tools
// should never access. Entries starting with ~ are expanded at runtime.
var DefaultSensitivePaths = []string{
	"~/.ssh",
	"~/.aws",
	"~/.gnupg",
	"~/.config/gcloud",
	"~/.kube",
	"~/.docker",
	"~/.config/gh",
	"/etc/shadow",
	"/etc/passwd",
	"secrets.yaml",
	"cogitator.yaml",
	"cogitator.db",
	"mcp.json",
}

// ExpandHome replaces a leading ~ with the user's home directory.
// Returns the path unchanged if it does not start with ~.
func ExpandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// ExpandPaths returns a new slice with every path ~ expanded.
func ExpandPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = ExpandHome(p)
	}
	return out
}

// ContainsSensitivePath reports whether s references any sensitive path.
// It checks both the raw pattern (with ~) and its home-expanded form.
// Returns the matched pattern or empty string.
func ContainsSensitivePath(s string, patterns []string) (bool, string) {
	for _, pat := range patterns {
		expanded := ExpandHome(pat)
		// Check raw form (e.g. ~/.ssh) and expanded form (e.g. /Users/x/.ssh).
		if containsPath(s, pat) || (expanded != pat && containsPath(s, expanded)) {
			return true, pat
		}
	}
	return false, ""
}

// IsSensitivePath checks whether abs falls under any sensitive directory or
// matches a sensitive filename. abs must be a clean, absolute path. Patterns
// may contain raw ~ forms (expanded internally) or bare filenames like
// "cogitator.db" which match by basename at any depth.
func IsSensitivePath(abs string, patterns []string) (bool, string) {
	for _, pat := range patterns {
		expanded := ExpandHome(pat)
		if abs == expanded || strings.HasPrefix(abs, expanded+string(filepath.Separator)) {
			return true, pat
		}
		// Bare filename patterns (no path separator) match by basename.
		if !strings.Contains(expanded, string(filepath.Separator)) {
			if filepath.Base(abs) == expanded {
				return true, pat
			}
		}
	}
	return false, ""
}

// containsPath checks if s contains path as a path-aligned substring.
// It ensures the match is not a partial directory name
// (e.g. ~/.sshconfig should not match ~/.ssh).
func containsPath(s, path string) bool {
	idx := strings.Index(s, path)
	if idx < 0 {
		return false
	}
	end := idx + len(path)
	if end >= len(s) {
		return true
	}
	next := s[end]
	return next == '/' || next == ' ' || next == '\'' || next == '"' || next == ';' || next == '|' || next == '&' || next == ')'
}
