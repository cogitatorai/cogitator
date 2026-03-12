package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadContent(t *testing.T) {
	dir := t.TempDir()
	cm := NewContentManager(dir)

	path, err := cm.Write("test-node-id", "# Some Memory\n\nThis is important.")
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	content, err := cm.Read(path)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if content != "# Some Memory\n\nThis is important." {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestDeleteContent(t *testing.T) {
	dir := t.TempDir()
	cm := NewContentManager(dir)

	path, _ := cm.Write("delete-me", "content")
	err := cm.Delete(path)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err = cm.Read(path)
	if err == nil {
		t.Error("expected error reading deleted content")
	}
}

func TestWriteCreatesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	cm := NewContentManager(dir)

	path, err := cm.Write("abc123", "content")
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	full := filepath.Join(dir, path)
	if _, err := os.Stat(full); err != nil {
		t.Errorf("expected file to exist at %s: %v", full, err)
	}

	// Verify sharding: should be in "ab/" directory
	if filepath.Dir(path) != "ab" {
		t.Errorf("expected shard directory 'ab', got %q", filepath.Dir(path))
	}
}

func TestWriteOverwrite(t *testing.T) {
	dir := t.TempDir()
	cm := NewContentManager(dir)

	cm.Write("overwrite-id", "original")
	cm.Write("overwrite-id", "updated")

	content, _ := cm.Read(filepath.Join("ov", "overwrite-id.md"))
	if content != "updated" {
		t.Errorf("expected 'updated', got %q", content)
	}
}

func TestReadNonexistent(t *testing.T) {
	dir := t.TempDir()
	cm := NewContentManager(dir)

	_, err := cm.Read("no/such/file.md")
	if err == nil {
		t.Error("expected error reading nonexistent file")
	}
}
