package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TargetInfo is a single target from CDP Target.getTargets.
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

// DiscoverWSURL finds a Chrome WebSocket debugger URL by reading the
// DevToolsActivePort file. Requires Chrome 146+ with debugging enabled
// via chrome://inspect/#remote-debugging.
func DiscoverWSURL() (wsURL string, err error) {
	if ws := ReadDevToolsActivePort(); ws != "" {
		return ws, nil
	}
	return "", fmt.Errorf("Chrome not reachable. Requires Chrome 146 or newer with debugging enabled in chrome://inspect/#remote-debugging")
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

