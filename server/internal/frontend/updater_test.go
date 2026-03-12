package frontend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// buildTarGz creates a tar.gz archive in memory from a map of filename->content.
func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func TestAtomicSwap(t *testing.T) {
	base := t.TempDir()
	publicDir := filepath.Join(base, "public")
	newDir := filepath.Join(base, "public.new")

	// Set up current public dir with a file.
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "old.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up new dir with a different file.
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := atomicSwap(publicDir, newDir); err != nil {
		t.Fatalf("atomicSwap: %v", err)
	}

	// publicDir should now contain new.txt, not old.txt.
	if _, err := os.Stat(filepath.Join(publicDir, "new.txt")); err != nil {
		t.Error("expected new.txt to exist in public dir after swap")
	}
	if _, err := os.Stat(filepath.Join(publicDir, "old.txt")); err == nil {
		t.Error("expected old.txt to NOT exist in public dir after swap")
	}

	// .old directory should be cleaned up.
	oldDir := publicDir + ".old"
	if _, err := os.Stat(oldDir); err == nil {
		t.Error("expected .old directory to be removed after swap")
	}

	// newDir should no longer exist (it became publicDir).
	if _, err := os.Stat(newDir); err == nil {
		t.Error("expected .new directory to no longer exist after swap")
	}
}

func TestAtomicSwap_NoExistingPublic(t *testing.T) {
	base := t.TempDir()
	publicDir := filepath.Join(base, "public")
	newDir := filepath.Join(base, "public.new")

	// No existing public dir, only new.
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "index.html"), []byte("<html>"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := atomicSwap(publicDir, newDir); err != nil {
		t.Fatalf("atomicSwap: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(publicDir, "index.html"))
	if err != nil {
		t.Fatal("expected index.html to exist after swap")
	}
	if string(data) != "<html>" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestExtractTarGz(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"index.html":      "<html>hello</html>",
		"assets/style.css": "body{}",
	})

	dest := filepath.Join(t.TempDir(), "extracted")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>hello</html>" {
		t.Errorf("unexpected content: %s", data)
	}

	data, err = os.ReadFile(filepath.Join(dest, "assets/style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "body{}" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestExtractTarGz_PathTraversal(t *testing.T) {
	// Build a malicious archive with path traversal.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "../../../etc/evil",
		Mode: 0644,
		Size: 4,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("pwnd"))
	tw.Close()
	gw.Close()

	dest := filepath.Join(t.TempDir(), "extracted")
	err := extractTarGz(buf.Bytes(), dest)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestExtractTarGz_AbsolutePathBlocked(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "/etc/passwd",
		Mode: 0644,
		Size: 4,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("root"))
	tw.Close()
	gw.Close()

	dest := filepath.Join(t.TempDir(), "extracted")
	err := extractTarGz(buf.Bytes(), dest)
	if err == nil {
		t.Fatal("expected error for absolute path in archive, got nil")
	}
}

func TestDownloadAndSwap_Success(t *testing.T) {
	files := map[string]string{
		"index.html": "<html>v2</html>",
		"app.js":     "console.log('v2')",
	}
	tarball := buildTarGz(t, files)
	hash := fmt.Sprintf("%x", sha256.Sum256(tarball))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer srv.Close()

	base := t.TempDir()
	publicDir := filepath.Join(base, "public")

	// Create existing public dir.
	os.MkdirAll(publicDir, 0755)
	os.WriteFile(filepath.Join(publicDir, "old.html"), []byte("old"), 0644)

	if err := DownloadAndSwap(publicDir, srv.URL+"/dashboard.tar.gz", hash); err != nil {
		t.Fatalf("DownloadAndSwap: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(publicDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>v2</html>" {
		t.Errorf("unexpected content: %s", data)
	}

	// Old file should be gone.
	if _, err := os.Stat(filepath.Join(publicDir, "old.html")); err == nil {
		t.Error("expected old.html to be gone after swap")
	}

	// Temp artifacts should be cleaned up.
	if _, err := os.Stat(publicDir + ".new"); err == nil {
		t.Error("expected .new dir to be cleaned up")
	}
	if _, err := os.Stat(publicDir + ".old"); err == nil {
		t.Error("expected .old dir to be cleaned up")
	}
}

func TestDownloadAndSwap_SHA256Mismatch(t *testing.T) {
	tarball := buildTarGz(t, map[string]string{"index.html": "content"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer srv.Close()

	base := t.TempDir()
	publicDir := filepath.Join(base, "public")
	os.MkdirAll(publicDir, 0755)

	err := DownloadAndSwap(publicDir, srv.URL+"/bad.tar.gz", "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected SHA256 mismatch error")
	}

	// Public dir should be untouched (still exist, no swap happened).
	if _, err := os.Stat(publicDir); err != nil {
		t.Error("public dir should still exist after failed swap")
	}
}

func TestDownloadAndSwap_BadURL(t *testing.T) {
	base := t.TempDir()
	publicDir := filepath.Join(base, "public")
	os.MkdirAll(publicDir, 0755)

	err := DownloadAndSwap(publicDir, "http://127.0.0.1:1/nonexistent", "abc123")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
