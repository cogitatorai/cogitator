package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainsSensitivePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		input   string
		want    bool
		wantPat string
	}{
		{
			name:    "direct ~/.ssh reference",
			input:   "cat ~/.ssh/id_rsa",
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:    "expanded home path",
			input:   "cat " + filepath.Join(home, ".ssh/id_rsa"),
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:    "aws credentials",
			input:   "cat ~/.aws/credentials",
			want:    true,
			wantPat: "~/.aws",
		},
		{
			name:    "etc shadow",
			input:   "cat /etc/shadow",
			want:    true,
			wantPat: "/etc/shadow",
		},
		{
			name:    "path traversal",
			input:   "cat ../../" + filepath.Join(home, ".ssh/known_hosts"),
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:  "safe command",
			input: "ls -la /tmp",
			want:  false,
		},
		{
			name:  "partial match should not trigger",
			input: "cat ~/.sshconfig",
			want:  false,
		},
		{
			name:  "partial match .awsome",
			input: "ls ~/.awsome",
			want:  false,
		},
		{
			name:    "pipe after sensitive path",
			input:   "cat ~/.ssh/id_rsa|base64",
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:    "semicolon after sensitive path",
			input:   "cat ~/.aws/credentials;echo done",
			want:    true,
			wantPat: "~/.aws",
		},
		{
			name:    "gnupg directory",
			input:   "ls ~/.gnupg/private-keys-v1.d",
			want:    true,
			wantPat: "~/.gnupg",
		},
		{
			name:    "cogitator database",
			input:   "cat ../cogitator.db",
			want:    true,
			wantPat: "cogitator.db",
		},
		{
			name:    "mcp config",
			input:   "cat ../mcp.json",
			want:    true,
			wantPat: "mcp.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := ContainsSensitivePath(tt.input, DefaultSensitivePaths)
			if got != tt.want {
				t.Errorf("ContainsSensitivePath(%q) = %v, want %v", tt.input, got, tt.want)
			}
			if tt.want && pat != tt.wantPat {
				t.Errorf("matched pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestIsSensitivePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	patterns := DefaultSensitivePaths

	tests := []struct {
		name    string
		abs     string
		want    bool
		wantPat string
	}{
		{
			name:    "exact ssh dir",
			abs:     filepath.Join(home, ".ssh"),
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:    "file inside ssh dir",
			abs:     filepath.Join(home, ".ssh", "id_rsa"),
			want:    true,
			wantPat: "~/.ssh",
		},
		{
			name:    "aws credentials",
			abs:     filepath.Join(home, ".aws", "credentials"),
			want:    true,
			wantPat: "~/.aws",
		},
		{
			name:    "etc shadow",
			abs:     "/etc/shadow",
			want:    true,
			wantPat: "/etc/shadow",
		},
		{
			name: "safe path",
			abs:  "/tmp/safe-file.txt",
			want: false,
		},
		{
			name: "partial match not triggered",
			abs:  filepath.Join(home, ".sshconfig"),
			want: false,
		},
		{
			name:    "cogitator.yaml by basename",
			abs:     filepath.Join(home, ".cogitator", "cogitator.yaml"),
			want:    true,
			wantPat: "cogitator.yaml",
		},
		{
			name:    "secrets.yaml by basename",
			abs:     "/data/workspace/secrets.yaml",
			want:    true,
			wantPat: "secrets.yaml",
		},
		{
			name:    "cogitator.db by basename",
			abs:     filepath.Join(home, ".cogitator", "cogitator.db"),
			want:    true,
			wantPat: "cogitator.db",
		},
		{
			name:    "mcp.json by basename",
			abs:     filepath.Join(home, ".cogitator", "mcp.json"),
			want:    true,
			wantPat: "mcp.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := IsSensitivePath(tt.abs, patterns)
			if got != tt.want {
				t.Errorf("IsSensitivePath(%q) = %v, want %v", tt.abs, got, tt.want)
			}
			if tt.want && pat != tt.wantPat {
				t.Errorf("matched pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	got := ExpandHome("~/.ssh")
	want := filepath.Join(home, ".ssh")
	if got != want {
		t.Errorf("ExpandHome(~/.ssh) = %q, want %q", got, want)
	}

	got = ExpandHome("/etc/shadow")
	if got != "/etc/shadow" {
		t.Errorf("ExpandHome(/etc/shadow) = %q, want /etc/shadow", got)
	}
}
