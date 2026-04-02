// Package frontend provides utilities for hot-swapping the dashboard
// static files served from disk. Used in SaaS mode to update the
// frontend without restarting the application.
package frontend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxFileSize is the maximum size of a single file extracted from a tarball (50 MB).
const maxFileSize = 50 << 20

// maxTarballSize is the maximum size of a downloaded tarball (100 MB).
const maxTarballSize = 100 << 20

// DownloadAndSwap downloads a .tar.gz from url, verifies its SHA256 hash
// matches expectedSHA256, extracts the contents, and atomically replaces
// publicDir with the new files. If any step fails, publicDir is left untouched.
func DownloadAndSwap(publicDir, url, expectedSHA256 string) error {
	// Download the tarball, hashing as we go.
	// Use a client that does not follow redirects to prevent SSRF.
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirectClient.Get(url)
	if err != nil {
		return fmt.Errorf("download frontend tarball: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download frontend tarball: HTTP %d", resp.StatusCode)
	}

	hasher := sha256.New()
	var buf bytes.Buffer
	limited := io.LimitReader(resp.Body, maxTarballSize)
	if _, err := io.Copy(&buf, io.TeeReader(limited, hasher)); err != nil {
		return fmt.Errorf("read frontend tarball: %w", err)
	}
	if buf.Len() >= maxTarballSize {
		return fmt.Errorf("frontend tarball exceeds %d byte limit", maxTarballSize)
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotHash, expectedSHA256) {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA256, gotHash)
	}

	// Extract to publicDir.new.
	newDir := publicDir + ".new"
	os.RemoveAll(newDir) // clean up any stale .new from a previous failed attempt
	if err := extractTarGz(buf.Bytes(), newDir); err != nil {
		os.RemoveAll(newDir)
		return fmt.Errorf("extract frontend tarball: %w", err)
	}

	if err := atomicSwap(publicDir, newDir); err != nil {
		os.RemoveAll(newDir)
		return fmt.Errorf("swap frontend directory: %w", err)
	}

	return nil
}

// atomicSwap replaces publicDir with newDir. It renames publicDir to
// publicDir.old, renames newDir to publicDir, then removes .old.
// If publicDir does not exist yet, it simply renames newDir into place.
func atomicSwap(publicDir, newDir string) error {
	oldDir := publicDir + ".old"

	// Clean up any stale .old from a previous run.
	os.RemoveAll(oldDir)

	// Move current public out of the way (if it exists).
	if _, err := os.Stat(publicDir); err == nil {
		if err := os.Rename(publicDir, oldDir); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", publicDir, oldDir, err)
		}
	}

	// Swing new into place.
	if err := os.Rename(newDir, publicDir); err != nil {
		// Attempt recovery: move old back.
		if _, statErr := os.Stat(oldDir); statErr == nil {
			os.Rename(oldDir, publicDir)
		}
		return fmt.Errorf("rename %s -> %s: %w", newDir, publicDir, err)
	}

	// Best-effort cleanup of the old directory.
	os.RemoveAll(oldDir)

	return nil
}

// extractTarGz extracts a gzip-compressed tar archive from data into destDir.
// It prevents path traversal by rejecting entries whose cleaned path escapes destDir.
func extractTarGz(data []byte, destDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		target, err := sanitizePath(destDir, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0755|0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize)); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// sanitizePath validates that name does not escape destDir via traversal
// or absolute paths. Returns the full resolved target path.
func sanitizePath(destDir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("archive contains absolute path: %s", name)
	}

	cleaned := filepath.Clean(name)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("archive contains path traversal: %s", name)
	}

	target := filepath.Join(destDir, cleaned)
	if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) &&
		target != filepath.Clean(destDir) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}

	return target, nil
}
