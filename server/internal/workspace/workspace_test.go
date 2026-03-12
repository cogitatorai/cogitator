package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()

	ws, err := Init(dir)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	for _, sub := range []string{
		"content/memories",
		"content/transcripts",
		"skills/installed",
		"skills/learned",
		"tools/custom",
	} {
		path := filepath.Join(dir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}

	profilePath := filepath.Join(dir, "content", "profile.md")
	if _, err := os.Stat(profilePath); err != nil {
		t.Errorf("expected profile.md to exist: %v", err)
	}

	if ws.Root != dir {
		t.Errorf("expected root %s, got %s", dir, ws.Root)
	}
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "content"), 0o755)
	os.WriteFile(filepath.Join(dir, "content", "profile.md"), []byte("# Custom"), 0o644)

	ws, err := Init(dir)
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	data, _ := os.ReadFile(ws.ProfilePath())
	if string(data) != "# Custom" {
		t.Errorf("expected existing profile to be preserved, got %q", data)
	}
}

func TestPaths(t *testing.T) {
	ws := &Workspace{Root: "/data"}

	tests := []struct {
		name   string
		got    string
		expect string
	}{
		{"ProfilePath", ws.ProfilePath(), "/data/content/profile.md"},
		{"MemoriesDir", ws.MemoriesDir(), "/data/content/memories"},
		{"TranscriptsDir", ws.TranscriptsDir(), "/data/content/transcripts"},
		{"SkillsInstalledDir", ws.SkillsInstalledDir(), "/data/skills/installed"},
		{"SkillsLearnedDir", ws.SkillsLearnedDir(), "/data/skills/learned"},
		{"CustomToolsDir", ws.CustomToolsDir(), "/data/tools/custom"},
		{"DBPath", ws.DBPath(), "/data/cogitator.db"},
	}

	for _, tt := range tests {
		if tt.got != tt.expect {
			t.Errorf("%s: expected %s, got %s", tt.name, tt.expect, tt.got)
		}
	}
}
