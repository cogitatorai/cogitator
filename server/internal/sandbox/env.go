package sandbox

import (
	"os"
	"strings"
)

// sensitiveEnvSubstrings lists substrings that, when found in an
// uppercased environment variable name, indicate a secret.
var sensitiveEnvSubstrings = []string{
	"API_KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"CREDENTIAL",
	"PRIVATE_KEY",
	"AWS_ACCESS",
	"AWS_SECRET",
}

// ScrubEnv returns a copy of the current environment with sensitive
// variables removed. A variable is considered sensitive if its
// uppercased name contains any of the sensitiveEnvSubstrings or
// starts with "COGITATOR_".
func ScrubEnv() []string {
	var clean []string
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if isSensitiveVar(name) {
			continue
		}
		clean = append(clean, entry)
	}
	return clean
}

func isSensitiveVar(name string) bool {
	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "COGITATOR_") {
		return true
	}
	for _, sub := range sensitiveEnvSubstrings {
		if strings.Contains(upper, sub) {
			return true
		}
	}
	return false
}
