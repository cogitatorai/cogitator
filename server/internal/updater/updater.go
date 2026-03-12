package updater

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config holds the settings for the updater.
type Config struct {
	Owner          string // GitHub owner (e.g. "deiu")
	Repo           string // GitHub repo (e.g. "cogi")
	Current        string // Current version (e.g. "v0.1.0" or "dev")
	Token          string // GitHub personal access token (required for private repos)
	CachePath      string // Path to update_cache.json (empty disables caching)
	SkippedVersion string // Version the user chose to skip
}

// ReleaseInfo describes a GitHub release.
type ReleaseInfo struct {
	Tag        string `json:"tag"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	AssetURL   string `json:"asset_url"`
	AssetName  string `json:"asset_name"`
	PublishedAt string `json:"published_at"`
}

// Status is the current state of the updater.
type Status struct {
	Current         string       `json:"current"`
	Latest          *ReleaseInfo `json:"latest,omitempty"`
	UpdateAvailable bool         `json:"update_available"`
	Checking        bool         `json:"checking"`
	Downloading     bool         `json:"downloading"`
	Ready           bool         `json:"ready"`
	Error           string       `json:"error,omitempty"`
	SkippedVersion  string       `json:"skipped_version,omitempty"`
}

// releaseCache is the on-disk format for persisting the latest release info.
type releaseCache struct {
	Latest          *ReleaseInfo `json:"latest,omitempty"`
	UpdateAvailable bool         `json:"update_available"`
}

// Updater periodically checks GitHub for new releases.
type Updater struct {
	cfg    Config
	client   *http.Client
	dlClient *http.Client

	mu         sync.RWMutex
	status     Status
	stop       chan struct{}
	newAppPath string // path to extracted .app ready for swap
	extractDir string // temp dir to clean up
}

// New creates an Updater. It loads any cached release info from disk so the
// dashboard can show the update banner immediately on restart.
func New(cfg Config) *Updater {
	u := &Updater{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
		dlClient: &http.Client{Timeout: 10 * time.Minute},
		status: Status{
			Current:        cfg.Current,
			SkippedVersion: cfg.SkippedVersion,
		},
		stop: make(chan struct{}),
	}
	u.loadCache()
	return u
}

// Start begins the background check loop. First check after 5 seconds, then every 30 minutes.
func (u *Updater) Start(ctx context.Context) {
	go func() {
		if u.cfg.Current == "dev" {
			log.Printf("updater: skipping checks (dev build)")
			return
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				u.CheckNow()
				timer.Reset(30 * time.Minute)
			case <-u.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop signals the background loop to exit.
func (u *Updater) Stop() {
	select {
	case u.stop <- struct{}{}:
	default:
	}
}

// Status returns the current updater state.
func (u *Updater) Status() Status {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.status
}

// githubRelease is the subset of the GitHub releases API response we use.
type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckNow performs an immediate check against GitHub.
func (u *Updater) CheckNow() {
	u.mu.Lock()
	u.status.Checking = true
	u.status.Error = ""
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.status.Checking = false
		u.mu.Unlock()
	}()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", u.cfg.Owner, u.cfg.Repo)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if u.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+u.cfg.Token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		u.setError(fmt.Sprintf("fetch release: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		u.mu.RLock()
		hadRelease := u.status.Latest != nil
		u.mu.RUnlock()
		if hadRelease {
			log.Printf("updater: GitHub returned 404 for releases (previously saw releases); verify the release exists on cogitatorai/cogitator")
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		u.setError(fmt.Sprintf("github api: %s", resp.Status))
		return
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		u.setError(fmt.Sprintf("decode release: %v", err))
		return
	}

	asset := u.findAsset(rel.Assets)
	info := &ReleaseInfo{
		Tag:         rel.TagName,
		Name:        rel.Name,
		URL:         rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}
	if asset != nil {
		// For private repos, use the API endpoint which accepts token auth.
		// The browser_download_url redirects through a signed URL that may not
		// work with Bearer tokens.
		if u.cfg.Token != "" {
			info.AssetURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/assets/%d",
				u.cfg.Owner, u.cfg.Repo, asset.ID)
		} else {
			info.AssetURL = asset.BrowserDownloadURL
		}
		info.AssetName = asset.Name
	}

	latest, err := parseSemver(rel.TagName)
	if err != nil {
		u.setError(fmt.Sprintf("parse latest version: %v", err))
		return
	}
	current, err := parseSemver(u.cfg.Current)
	if err != nil {
		u.setError(fmt.Sprintf("parse current version: %v", err))
		return
	}

	u.mu.Lock()
	u.status.Latest = info
	u.status.UpdateAvailable = latest.newerThan(current)
	u.mu.Unlock()

	u.saveCache()

	if u.status.UpdateAvailable {
		log.Printf("updater: new version available: %s (current: %s)", rel.TagName, u.cfg.Current)
	}
}

// findAsset picks the best matching zip asset for the current architecture.
func (u *Updater) findAsset(assets []githubAsset) *githubAsset {
	arch := runtime.GOARCH
	if arch == "arm64" {
		arch = "arm64"
	} else {
		arch = "x86_64"
	}

	// Prefer architecture-specific, fall back to universal.
	var universal *githubAsset
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if !strings.HasPrefix(name, "cogitator-") || !strings.HasSuffix(name, ".zip") {
			continue
		}
		if strings.Contains(name, "universal") {
			universal = &assets[i]
		}
		if strings.Contains(name, arch) {
			return &assets[i]
		}
	}
	return universal
}

func (u *Updater) setError(msg string) {
	u.mu.Lock()
	u.status.Error = msg
	u.mu.Unlock()
	log.Printf("updater: %s", msg)
}

// loadCache reads cached release info from disk. Missing or corrupt files
// are silently ignored so the updater falls back to a fresh GitHub check.
func (u *Updater) loadCache() {
	if u.cfg.CachePath == "" {
		return
	}
	data, err := os.ReadFile(u.cfg.CachePath)
	if err != nil {
		return
	}
	var c releaseCache
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	u.status.Latest = c.Latest

	// Revalidate against current version; the cache may be stale after an upgrade.
	if c.Latest != nil {
		latest, errL := parseSemver(c.Latest.Tag)
		current, errC := parseSemver(u.cfg.Current)
		if errL == nil && errC == nil {
			u.status.UpdateAvailable = latest.newerThan(current)
		} else {
			u.status.UpdateAvailable = false
		}
	} else {
		u.status.UpdateAvailable = false
	}
}

// saveCache writes the current latest release info to disk so the banner
// can appear instantly after a restart.
func (u *Updater) saveCache() {
	if u.cfg.CachePath == "" {
		return
	}
	u.mu.RLock()
	c := releaseCache{
		Latest:          u.status.Latest,
		UpdateAvailable: u.status.UpdateAvailable,
	}
	u.mu.RUnlock()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(u.cfg.CachePath, data, 0644)
}

// SetSkippedVersion records the version the user chose to skip.
func (u *Updater) SetSkippedVersion(v string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.status.SkippedVersion = v
}

// Download fetches and extracts the latest release to a temp directory.
// Once complete, Status.Ready becomes true and the user can call Restart.
func (u *Updater) Download() error {
	u.mu.RLock()
	st := u.status
	u.mu.RUnlock()

	if !st.UpdateAvailable || st.Latest == nil || st.Latest.AssetURL == "" {
		return fmt.Errorf("no update available or no compatible asset")
	}
	if st.Ready {
		// Verify the previously downloaded files still exist on disk.
		u.mu.RLock()
		path := u.newAppPath
		u.mu.RUnlock()
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				return nil // already downloaded and valid
			}
		}
		// Files are gone; reset and re-download.
		u.mu.Lock()
		u.status.Ready = false
		u.newAppPath = ""
		u.extractDir = ""
		u.mu.Unlock()
	}

	u.mu.Lock()
	u.status.Downloading = true
	u.status.Error = ""
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.status.Downloading = false
		u.mu.Unlock()
	}()

	// 1. Download zip to temp file.
	tmpZip := filepath.Join(os.TempDir(), st.Latest.AssetName)
	if err := u.downloadFile(st.Latest.AssetURL, tmpZip); err != nil {
		u.setError(fmt.Sprintf("download: %v", err))
		return fmt.Errorf("download: %w", err)
	}

	// 2. Extract to temp dir.
	extractDir, err := os.MkdirTemp("", "cogitator-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	if err := extractZip(tmpZip, extractDir); err != nil {
		os.RemoveAll(extractDir)
		return fmt.Errorf("extract: %w", err)
	}
	os.Remove(tmpZip)

	// 3. Validate the extracted bundle.
	newApp := filepath.Join(extractDir, "Cogitator.app")
	serverBin := filepath.Join(newApp, "Contents", "MacOS", "cogitator-server")
	if _, err := os.Stat(serverBin); err != nil {
		os.RemoveAll(extractDir)
		return fmt.Errorf("extracted bundle missing cogitator-server: %w", err)
	}

	u.mu.Lock()
	u.newAppPath = newApp
	u.extractDir = extractDir
	u.status.Ready = true
	u.mu.Unlock()

	log.Printf("updater: %s downloaded and ready to install", st.Latest.Tag)
	return nil
}

// Restart writes the swap script, launches it detached, and exits the process.
// Call only after Download has completed (Status.Ready is true).
func (u *Updater) Restart(shutdownFn func()) error {
	u.mu.RLock()
	ready := u.status.Ready
	newApp := u.newAppPath
	extractDir := u.extractDir
	tag := ""
	if u.status.Latest != nil {
		tag = u.status.Latest.Tag
	}
	u.mu.RUnlock()

	if !ready || newApp == "" {
		return fmt.Errorf("no update downloaded; call Download first")
	}

	currentApp, err := appBundlePath()
	if err != nil {
		return fmt.Errorf("detect current app: %w", err)
	}

	pid := os.Getpid()
	script := fmt.Sprintf(`#!/bin/bash
set -e

# Wait for the server to exit.
while kill -0 %d 2>/dev/null; do sleep 0.2; done

# Backup old bundle.
BACKUP="%s.backup"
rm -rf "$BACKUP"
mv "%s" "$BACKUP"

# Move new bundle into place.
mv "%s" "%s"

# Relaunch.
open "%s"

# Clean up.
rm -rf "$BACKUP"
rm -rf "%s"
rm -f "$0"
`, pid, currentApp, currentApp, newApp, currentApp, currentApp, extractDir)

	scriptPath := filepath.Join(os.TempDir(), "cogitator-update.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write update script: %w", err)
	}

	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch update script: %w", err)
	}

	log.Printf("updater: restarting for update to %s", tag)
	go func() {
		shutdownFn()
		os.Exit(0)
	}()
	return nil
}

// appBundlePath resolves the path to the current .app bundle by walking up from the executable.
func appBundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	// Walk up: .../Cogitator.app/Contents/MacOS/cogitator-server -> .../Cogitator.app
	dir := exe
	for i := 0; i < 4; i++ {
		dir = filepath.Dir(dir)
		if strings.HasSuffix(dir, ".app") {
			return dir, nil
		}
	}
	return "", fmt.Errorf("could not find .app bundle from executable path: %s", exe)
}

func (u *Updater) downloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if u.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+u.cfg.Token)
	}
	resp, err := u.dlClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		// Guard against zip slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
