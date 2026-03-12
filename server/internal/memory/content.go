package memory

import (
	"os"
	"path/filepath"
)

type ContentManager struct {
	baseDir string
}

func NewContentManager(baseDir string) *ContentManager {
	return &ContentManager{baseDir: baseDir}
}

// Write stores content for a node. Returns the relative path from baseDir.
// Files are sharded by the first 2 chars of the node ID to avoid
// huge flat directories.
func (cm *ContentManager) Write(nodeID, content string) (string, error) {
	shard := nodeID[:2]
	relDir := shard
	absDir := filepath.Join(cm.baseDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", err
	}

	relPath := filepath.Join(relDir, nodeID+".md")
	absPath := filepath.Join(cm.baseDir, relPath)

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return relPath, nil
}

func (cm *ContentManager) Read(relPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(cm.baseDir, relPath))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (cm *ContentManager) Delete(relPath string) error {
	return os.Remove(filepath.Join(cm.baseDir, relPath))
}
