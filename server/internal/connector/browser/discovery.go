package browser

import (
	"context"
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

// DiscoverWSURL finds a Chrome WebSocket debugger URL. When managed is false
// (external Chrome), it checks the DevToolsActivePort file first, then falls
// back to HTTP /json/version. When managed is true (Cogitator launched Chrome),
// it skips DevToolsActivePort (which may be stale) and goes straight to the
// HTTP endpoint on the configured port.
func DiscoverWSURL(configuredPort int, managed bool) (wsURL string, err error) {
	if !managed {
		// 1. Try DevToolsActivePort file (modern Chrome with debugging toggle).
		if ws := ReadDevToolsActivePort(); ws != "" {
			return ws, nil
		}
	}

	// 2. Try configured port via HTTP (--remote-debugging-port or managed headless).
	base := fmt.Sprintf("http://127.0.0.1:%d", configuredPort)
	info, err := GetVersion(base)
	if err != nil {
		if managed {
			return "", fmt.Errorf("managed chrome not reachable on port %d: %w", configuredPort, err)
		}
		return "", fmt.Errorf("chrome not reachable: enable debugging in chrome://inspect/#remote-debugging or start Chrome with --remote-debugging-port=%d", configuredPort)
	}
	return info.WebSocketDebuggerURL, nil
}

// ListTargetsCDP lists page targets via the CDP Target.getTargets command.
// This works even when Chrome's HTTP /json/list endpoint is unavailable.
func ListTargetsCDP(ctx context.Context, client *Client) ([]TargetInfo, error) {
	result, err := client.Send(ctx, "Target.getTargets", nil, "")
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	var resp struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse targets: %w", err)
	}
	var pages []TargetInfo
	for _, t := range resp.TargetInfos {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "chrome://") {
			pages = append(pages, TargetInfo{
				ID:    t.TargetID,
				Type:  t.Type,
				Title: t.Title,
				URL:   t.URL,
			})
		}
	}
	return pages, nil
}

// CreateTargetCDP opens a new tab via the CDP Target.createTarget command.
func CreateTargetCDP(ctx context.Context, client *Client, url string) (*TargetInfo, error) {
	result, err := client.Send(ctx, "Target.createTarget", map[string]any{
		"url": url,
	}, "")
	if err != nil {
		return nil, fmt.Errorf("create target: %w", err)
	}
	var resp struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse create target: %w", err)
	}
	return &TargetInfo{ID: resp.TargetID, Type: "page", URL: url}, nil
}

// GetVersionCDP fetches Chrome version via the CDP Browser.getVersion command.
func GetVersionCDP(ctx context.Context, client *Client) (string, error) {
	result, err := client.Send(ctx, "Browser.getVersion", nil, "")
	if err != nil {
		return "", err
	}
	var resp struct {
		Product string `json:"product"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	return resp.Product, nil
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
