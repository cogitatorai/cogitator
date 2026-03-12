package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	Root string
}

var subdirs = []string{
	"content/memories",
	"content/transcripts",
	"skills/installed",
	"skills/learned",
	"tools/custom",
	"sandbox",
	"connectors",
}

const defaultProfile = `## Communication
(No preferences learned yet.)

## Task Execution
(No preferences learned yet.)
`

func Init(root string) (*Workspace, error) {
	if strings.HasPrefix(root, "~/") || root == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, root[1:])
		}
	}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}

	profilePath := filepath.Join(root, "content", "profile.md")
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		if err := os.WriteFile(profilePath, []byte(defaultProfile), 0o644); err != nil {
			return nil, err
		}
	}

	return &Workspace{Root: root}, nil
}

func (w *Workspace) ProfilePath() string {
	return filepath.Join(w.Root, "content", "profile.md")
}

func (w *Workspace) MemoriesDir() string {
	return filepath.Join(w.Root, "content", "memories")
}

func (w *Workspace) TranscriptsDir() string {
	return filepath.Join(w.Root, "content", "transcripts")
}

func (w *Workspace) SkillsInstalledDir() string {
	return filepath.Join(w.Root, "skills", "installed")
}

func (w *Workspace) SkillsLearnedDir() string {
	return filepath.Join(w.Root, "skills", "learned")
}

func (w *Workspace) CustomToolsDir() string {
	return filepath.Join(w.Root, "tools", "custom")
}

func (w *Workspace) DBPath() string {
	return filepath.Join(w.Root, "cogitator.db")
}

func (w *Workspace) ConfigPath() string {
	return filepath.Join(w.Root, "cogitator.yaml")
}

// SandboxDir returns the directory where shell commands execute.
// This is a subdirectory of the workspace root so that config and
// secrets files in the root are not reachable via relative paths.
func (w *Workspace) SandboxDir() string {
	return filepath.Join(w.Root, "sandbox")
}

// ConnectorsDir returns the directory for user-installed connector packages.
func (w *Workspace) ConnectorsDir() string {
	return filepath.Join(w.Root, "connectors")
}

// MCPConfigPath returns the path to the MCP server configuration file.
func (w *Workspace) MCPConfigPath() string {
	return filepath.Join(w.Root, "mcp.json")
}
