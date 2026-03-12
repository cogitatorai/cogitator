package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/andybalholm/cascadia"
	"github.com/cogitatorai/cogitator/server/internal/security"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"golang.org/x/net/html"
)

const (
	fetchTimeout      = 30 * time.Second
	fetchMaxBody      = 2 << 20  // 2 MB
	fetchMaxOutput    = 32 << 10 // 32 KB
	fetchMaxRedirects = 5
	fetchUserAgent    = "Cogitator/1.0"
)


func (e *Executor) fetchURL(ctx context.Context, args string) (string, error) {
	var p struct {
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Raw      bool   `json:"raw"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.URL == "" {
		return "", fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
		return "", fmt.Errorf("url must start with http:// or https://")
	}

	parsed, err := url.Parse(p.URL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	host := parsed.Hostname()

	if isLocalOrPrivate(host) {
		return "", fmt.Errorf("access to local/private addresses is not allowed")
	}

	// Autonomous tasks require allowlisted domains.
	if task.ToolCallCollectorFromContext(ctx) != nil {
		allowed := false
		for _, pattern := range e.allowedDomains {
			if security.MatchesDomain(host, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("%s not in the allowed domains list. This domain must be allowlisted before the task runs", host)
		}
	}

	e.logAudit(ctx, "url_fetch", "fetch_url", p.URL, "allowed", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	truncatedRead := len(body) > fetchMaxBody
	if truncatedRead {
		body = body[:fetchMaxBody]
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		result := string(body)
		if resp.StatusCode >= 400 {
			result = fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, result)
		}
		return truncateOutput(result, truncatedRead), nil
	}

	htmlContent := string(body)

	if p.Selector != "" {
		selected, err := applySelector(htmlContent, p.Selector)
		if err != nil {
			return "", err
		}
		if selected == "" {
			return fmt.Sprintf("Selector %q matched nothing on %s. Try without a selector to see the full page.", p.Selector, p.URL), nil
		}
		htmlContent = selected
	}

	if p.Raw {
		result := htmlContent
		if resp.StatusCode >= 400 {
			result = fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, result)
		}
		return truncateOutput(result, truncatedRead), nil
	}

	md, err := htmltomarkdown.ConvertString(htmlContent)
	if err != nil {
		return truncateOutput(htmlContent, truncatedRead), nil
	}

	result := strings.TrimSpace(md)
	if resp.StatusCode >= 400 {
		result = fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, result)
	}
	return truncateOutput(result, truncatedRead), nil
}

func applySelector(htmlContent, selector string) (string, error) {
	sel, err := cascadia.Compile(selector)
	if err != nil {
		return "", fmt.Errorf("invalid CSS selector %q: %w", selector, err)
	}
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	matches := cascadia.QueryAll(doc, sel)
	if len(matches) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	for _, node := range matches {
		if err := html.Render(&buf, node); err != nil {
			return "", fmt.Errorf("failed to render selected HTML: %w", err)
		}
	}
	return buf.String(), nil
}

func isLocalOrPrivate(host string) bool {
	if strings.ToLower(host) == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func truncateOutput(s string, readTruncated bool) string {
	outputTruncated := false
	if len(s) > fetchMaxOutput {
		s = strings.ToValidUTF8(s[:fetchMaxOutput], "")
		outputTruncated = true
	}
	if readTruncated || outputTruncated {
		s += "\n\n[Content truncated]"
	}
	return s
}
