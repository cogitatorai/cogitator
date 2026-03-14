package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
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
