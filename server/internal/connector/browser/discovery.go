package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

// VersionInfo is the response from /json/version.
type VersionInfo struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// TargetInfo is a single target from /json/list.
type TargetInfo struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// devToolsActivePortPaths returns candidate paths for Chrome's DevToolsActivePort file.
// Chrome writes this file when the user enables debugging via chrome://inspect/#remote-debugging.
func devToolsActivePortPaths() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "DevToolsActivePort"),
		}
	case "linux":
		return []string{
			filepath.Join(home, ".config", "google-chrome", "DevToolsActivePort"),
			filepath.Join(home, ".config", "chromium", "DevToolsActivePort"),
		}
	default:
		return nil
	}
}

// ReadDevToolsActivePort reads the DevToolsActivePort file and returns the
// WebSocket debugger URL. The file contains two lines: the port number and
// the browser WebSocket path. Returns empty string if the file does not exist.
func ReadDevToolsActivePort() string {
	for _, path := range devToolsActivePortPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
		if len(lines) == 2 && lines[0] != "" && lines[1] != "" {
			return fmt.Sprintf("ws://127.0.0.1:%s%s", lines[0], lines[1])
		}
	}
	return ""
}

// DiscoverBaseURL finds a reachable Chrome debug endpoint. It checks the
// DevToolsActivePort file first (for Chrome with debugging toggled on via
// chrome://inspect), then falls back to the configured port.
func DiscoverBaseURL(configuredPort int) (baseURL string, wsURL string, err error) {
	// 1. Try DevToolsActivePort file (modern Chrome, no --remote-debugging-port needed).
	if ws := ReadDevToolsActivePort(); ws != "" {
		// Extract port from the WS URL to build the HTTP base URL.
		// Format: ws://127.0.0.1:{port}/devtools/browser/{id}
		parts := strings.SplitN(strings.TrimPrefix(ws, "ws://127.0.0.1:"), "/", 2)
		if len(parts) >= 1 {
			port := parts[0]
			base := "http://127.0.0.1:" + port
			if _, verr := GetVersion(base); verr == nil {
				return base, ws, nil
			}
		}
	}

	// 2. Try configured port (--remote-debugging-port or managed headless).
	base := fmt.Sprintf("http://127.0.0.1:%d", configuredPort)
	info, err := GetVersion(base)
	if err != nil {
		return "", "", err
	}
	return base, info.WebSocketDebuggerURL, nil
}

// GetVersion fetches Chrome version info. baseURL is like "http://127.0.0.1:9222".
func GetVersion(baseURL string) (*VersionInfo, error) {
	resp, err := httpClient.Get(baseURL + "/json/version")
	if err != nil {
		return nil, fmt.Errorf("chrome not reachable: %w", err)
	}
	defer resp.Body.Close()
	var info VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("invalid version response: %w", err)
	}
	return &info, nil
}

// ListTargets returns page targets, filtering out chrome:// and non-page types.
func ListTargets(baseURL string) ([]TargetInfo, error) {
	resp, err := httpClient.Get(baseURL + "/json/list")
	if err != nil {
		return nil, fmt.Errorf("chrome not reachable: %w", err)
	}
	defer resp.Body.Close()
	var all []TargetInfo
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("invalid targets response: %w", err)
	}
	var pages []TargetInfo
	for _, t := range all {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "chrome://") {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// CreateTarget opens a new tab. Returns the new target info.
func CreateTarget(baseURL, url string) (*TargetInfo, error) {
	req, err := http.NewRequest(http.MethodPut, baseURL+"/json/new?"+url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create target: %w", err)
	}
	defer resp.Body.Close()
	var info TargetInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("invalid create target response: %w", err)
	}
	return &info, nil
}
