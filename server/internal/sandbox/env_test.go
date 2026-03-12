package sandbox

import (
	"strings"
	"testing"
)

func TestIsSensitiveVar(t *testing.T) {
	tests := []struct {
		name      string
		sensitive bool
	}{
		{"OPENAI_API_KEY", true},
		{"MY_SECRET_VALUE", true},
		{"GITHUB_TOKEN", true},
		{"DB_PASSWORD", true},
		{"AWS_ACCESS_KEY_ID", true},
		{"AWS_SECRET_ACCESS_KEY", true},
		{"COGITATOR_SERVER_PORT", true},
		{"PRIVATE_KEY_PATH", true},
		{"MY_CREDENTIAL_FILE", true},
		{"PATH", false},
		{"HOME", false},
		{"LANG", false},
		{"SHELL", false},
		{"USER", false},
		{"TERM", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSensitiveVar(tt.name); got != tt.sensitive {
				t.Errorf("isSensitiveVar(%q) = %v, want %v", tt.name, got, tt.sensitive)
			}
		})
	}
}

func TestScrubEnv(t *testing.T) {
	// Set some env vars for the test.
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("COGITATOR_SERVER_PORT", "8484")
	t.Setenv("TEST_SAFE_VAR", "safe")

	scrubbed := ScrubEnv()

	for _, entry := range scrubbed {
		name, _, _ := strings.Cut(entry, "=")
		if isSensitiveVar(name) {
			t.Errorf("sensitive var %q not scrubbed", name)
		}
	}

	// Verify safe var is present.
	found := false
	for _, entry := range scrubbed {
		if strings.HasPrefix(entry, "TEST_SAFE_VAR=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("safe var TEST_SAFE_VAR was scrubbed incorrectly")
	}
}
