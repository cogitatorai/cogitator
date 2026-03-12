package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

const (
	searchDefaultCount = 5
	searchMaxCount     = 10
)

// searchResult holds a single search engine result.
type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// searchEngine abstracts a single search provider.
type searchEngine interface {
	Name() string
	SearchURL(query string) string
	ParseResults(doc *html.Node, count int) []searchResult
}

// defaultEngines is the rotation order for web searches.
// Tests may override this variable.
var defaultEngines = []searchEngine{
	&duckduckgoEngine{},
	&braveEngine{},
	&mojeekEngine{},
}

// ---------- DuckDuckGo HTML Lite ----------

type duckduckgoEngine struct{}

func (duckduckgoEngine) Name() string { return "DuckDuckGo" }

func (duckduckgoEngine) SearchURL(query string) string {
	return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
}

var (
	ddgContainer = mustCompile("div.result")
	ddgTitle     = mustCompile("a.result__a")
	ddgSnippet   = mustCompile("a.result__snippet")
)

func (duckduckgoEngine) ParseResults(doc *html.Node, count int) []searchResult {
	containers := cascadia.QueryAll(doc, ddgContainer)
	var results []searchResult
	for _, c := range containers {
		if len(results) >= count {
			break
		}
		titleNode := cascadia.Query(c, ddgTitle)
		if titleNode == nil {
			continue
		}
		title := strings.TrimSpace(textContent(titleNode))
		href := attrVal(titleNode, "href")
		if title == "" || href == "" {
			continue
		}
		snippet := ""
		if sn := cascadia.Query(c, ddgSnippet); sn != nil {
			snippet = strings.TrimSpace(textContent(sn))
		}
		results = append(results, searchResult{Title: title, URL: href, Snippet: snippet})
	}
	return results
}

// ---------- Brave Search ----------

type braveEngine struct{}

func (braveEngine) Name() string { return "Brave" }

func (braveEngine) SearchURL(query string) string {
	return "https://search.brave.com/search?q=" + url.QueryEscape(query)
}

var (
	braveContainer    = mustCompile("div.snippet[data-type=\"web\"]")
	braveTitle        = mustCompile("a.heading-serpresult")
	braveSnippet      = mustCompile("div.snippet-description")
	braveAltContainer = mustCompile("div.fdb")
	braveAltTitle     = mustCompile("a.result-header")
	braveAltSnippet   = mustCompile("p.snippet-description")
)

func (b braveEngine) ParseResults(doc *html.Node, count int) []searchResult {
	results := b.parsePattern(doc, count, braveContainer, braveTitle, braveSnippet)
	if len(results) == 0 {
		results = b.parsePattern(doc, count, braveAltContainer, braveAltTitle, braveAltSnippet)
	}
	return results
}

func (braveEngine) parsePattern(doc *html.Node, count int, container, title, snippet cascadia.Selector) []searchResult {
	containers := cascadia.QueryAll(doc, container)
	var results []searchResult
	for _, c := range containers {
		if len(results) >= count {
			break
		}
		titleNode := cascadia.Query(c, title)
		if titleNode == nil {
			continue
		}
		t := strings.TrimSpace(textContent(titleNode))
		href := attrVal(titleNode, "href")
		if t == "" || href == "" {
			continue
		}
		s := ""
		if sn := cascadia.Query(c, snippet); sn != nil {
			s = strings.TrimSpace(textContent(sn))
		}
		results = append(results, searchResult{Title: t, URL: href, Snippet: s})
	}
	return results
}

// ---------- Mojeek ----------

type mojeekEngine struct{}

func (mojeekEngine) Name() string { return "Mojeek" }

func (mojeekEngine) SearchURL(query string) string {
	return "https://www.mojeek.com/search?q=" + url.QueryEscape(query)
}

var (
	mojeekContainer = mustCompile("ul.results-standard > li")
	mojeekTitle     = mustCompile("a.ob")
	mojeekSnippet   = mustCompile("p.s")
)

func (mojeekEngine) ParseResults(doc *html.Node, count int) []searchResult {
	containers := cascadia.QueryAll(doc, mojeekContainer)
	var results []searchResult
	for _, c := range containers {
		if len(results) >= count {
			break
		}
		titleNode := cascadia.Query(c, mojeekTitle)
		if titleNode == nil {
			continue
		}
		title := strings.TrimSpace(textContent(titleNode))
		href := attrVal(titleNode, "href")
		if title == "" || href == "" {
			continue
		}
		snippet := ""
		if sn := cascadia.Query(c, mojeekSnippet); sn != nil {
			snippet = strings.TrimSpace(textContent(sn))
		}
		results = append(results, searchResult{Title: title, URL: href, Snippet: snippet})
	}
	return results
}

// ---------- Helpers ----------

// mustCompile compiles a CSS selector or panics. Used for package-level
// constants that are known at compile time.
func mustCompile(sel string) cascadia.Selector {
	s, err := cascadia.Compile(sel)
	if err != nil {
		panic(fmt.Sprintf("invalid selector %q: %v", sel, err))
	}
	return s
}

// textContent extracts all text from an HTML node tree, stripping tags.
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var buf strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		buf.WriteString(textContent(c))
	}
	return buf.String()
}

// attrVal returns the value of an attribute on an HTML node, or "" if missing.
func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// formatSearchResults builds a numbered markdown list from search results.
func formatSearchResults(query string, results []searchResult) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "## Search Results for %q\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&buf, "%d. **%s**\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&buf, "   %s\n", r.Snippet)
		}
		buf.WriteString("\n")
	}
	return strings.TrimRight(buf.String(), "\n")
}

// ---------- Executor entry point ----------

func (e *Executor) webSearch(ctx context.Context, args string) (string, error) {
	var p struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Count <= 0 {
		p.Count = searchDefaultCount
	}
	if p.Count > searchMaxCount {
		p.Count = searchMaxCount
	}

	engines := defaultEngines
	n := len(engines)
	if n == 0 {
		return "", fmt.Errorf("no search engines configured")
	}

	// Rotate starting engine so successive calls spread load.
	start := int(atomic.AddUint64(&e.searchCounter, 1)-1) % n

	var lastErr error
	for i := 0; i < n; i++ {
		eng := engines[(start+i)%n]
		results, err := e.executeSearch(ctx, eng, p.Query, p.Count)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) == 0 {
			continue
		}
		e.logAudit(ctx, "web_search", "web_search", p.Query, "allowed",
			fmt.Sprintf("engine=%s results=%d", eng.Name(), len(results)))
		return formatSearchResults(p.Query, results), nil
	}

	if lastErr != nil {
		return fmt.Sprintf("All search engines failed for %q. Last error: %v. Try using fetch_url to search a specific site directly.", p.Query, lastErr), nil
	}
	return fmt.Sprintf("No results found for %q across all search engines. Try using fetch_url to search a specific site directly.", p.Query), nil
}

// executeSearch fetches a single engine and parses the results.
func (e *Executor) executeSearch(ctx context.Context, eng searchEngine, query string, count int) ([]searchResult, error) {
	searchURL := eng.SearchURL(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, eng.Name())
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	return eng.ParseResults(doc, count), nil
}
