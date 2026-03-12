package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/cogitatorai/cogitator/server/internal/task"
)

// --- URL validation ---

func TestFetchURL_EmptyURL(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	args, _ := json.Marshal(map[string]any{"url": ""})
	_, err := exe.fetchURL(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected 'url is required' error, got: %v", err)
	}
}

func TestFetchURL_NoScheme(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	args, _ := json.Marshal(map[string]any{"url": "example.com"})
	_, err := exe.fetchURL(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected scheme error, got: %v", err)
	}
}

func TestFetchURL_FileScheme(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	args, _ := json.Marshal(map[string]any{"url": "file:///etc/passwd"})
	_, err := exe.fetchURL(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected scheme error, got: %v", err)
	}
}

func TestFetchURL_FTPScheme(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	args, _ := json.Marshal(map[string]any{"url": "ftp://files.example.com/data"})
	_, err := exe.fetchURL(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected scheme error, got: %v", err)
	}
}

// --- Local/private IP blocking ---

func TestFetchURL_BlocksLocalhost(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	for _, u := range []string{"http://localhost/path", "http://127.0.0.1/path", "http://[::1]/path"} {
		host := u // for error messages
		args, _ := json.Marshal(map[string]any{"url": u})
		_, err := exe.fetchURL(context.Background(), string(args))
		if err == nil || !strings.Contains(err.Error(), "local/private") {
			t.Errorf("host %s: expected local/private error, got: %v", host, err)
		}
	}
}

func TestFetchURL_BlocksPrivateIPs(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	for _, ip := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1"} {
		args, _ := json.Marshal(map[string]any{"url": fmt.Sprintf("http://%s/path", ip)})
		_, err := exe.fetchURL(context.Background(), string(args))
		if err == nil || !strings.Contains(err.Error(), "local/private") {
			t.Errorf("IP %s: expected local/private error, got: %v", ip, err)
		}
	}
}

// --- isLocalOrPrivate function ---

func TestIsLocalOrPrivate(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"8.8.8.8", false},
		{"93.184.216.34", false},
		{"example.com", false},     // non-IP hostname, not "localhost"
		{"LOCALHOST", true},        // case insensitive
		{"169.254.1.1", true},      // link-local
	}
	for _, tt := range tests {
		got := isLocalOrPrivate(tt.host)
		if got != tt.want {
			t.Errorf("isLocalOrPrivate(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

// --- Domain security: interactive vs autonomous ---

func TestFetchURL_InteractiveAllowsAnyDomain(t *testing.T) {
	// Interactive mode = no ToolCallCollector in context.
	// This will fail at the HTTP level (DNS), but should NOT fail with domain error.
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	args, _ := json.Marshal(map[string]any{"url": "http://nonexistent-domain-xyz.example.com"})
	_, err := exe.fetchURL(context.Background(), string(args))
	// Should get a fetch error, not a domain allowlist error.
	if err != nil && strings.Contains(err.Error(), "allowed domains") {
		t.Fatalf("interactive mode should not check domain allowlist, got: %v", err)
	}
}

func TestFetchURL_AutonomousBlocksUnlisted(t *testing.T) {
	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	collector := &task.ToolCallCollector{}
	ctx := task.WithToolCallCollector(context.Background(), collector)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/page"})
	_, err := exe.fetchURL(ctx, string(args))
	if err == nil || !strings.Contains(err.Error(), "allowed domains") {
		t.Fatalf("autonomous mode should block unlisted domain, got: %v", err)
	}
}

func TestFetchURL_AutonomousAllowsListed(t *testing.T) {
	// Domain is allowlisted but will fail at HTTP level (DNS). That's fine;
	// the point is it should NOT fail with a domain allowlist error.
	exe, _ := newTestExecutorWithAllowlist(t, &mockAuditLogger{}, []string{"example.com"})
	collector := &task.ToolCallCollector{}
	ctx := task.WithToolCallCollector(context.Background(), collector)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/page"})
	_, err := exe.fetchURL(ctx, string(args))
	if err != nil && strings.Contains(err.Error(), "allowed domains") {
		t.Fatalf("domain is allowlisted, should not get domain error, got: %v", err)
	}
}

// --- truncateOutput function ---

func TestTruncateOutput_Short(t *testing.T) {
	result := truncateOutput("hello", false)
	if result != "hello" {
		t.Fatalf("expected 'hello', got: %q", result)
	}
}

func TestTruncateOutput_ReadTruncated(t *testing.T) {
	result := truncateOutput("some content", true)
	if !strings.Contains(result, "[Content truncated]") {
		t.Fatalf("expected truncation notice, got: %q", result)
	}
}

func TestTruncateOutput_Oversized(t *testing.T) {
	big := strings.Repeat("x", fetchMaxOutput+100)
	result := truncateOutput(big, false)
	if !strings.Contains(result, "[Content truncated]") {
		t.Fatalf("expected truncation notice for oversized content")
	}
	// The truncated content should be fetchMaxOutput + the truncation notice.
	if len(result) > fetchMaxOutput+50 {
		t.Fatalf("output too large after truncation: %d", len(result))
	}
}

// --- CSS selector extraction ---

func TestApplySelector_Match(t *testing.T) {
	html := `<html><body><nav>menu</nav><main><p>content</p></main></body></html>`
	result, err := applySelector(html, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "content") {
		t.Fatalf("expected 'content' in result, got: %q", result)
	}
	if strings.Contains(result, "menu") {
		t.Fatalf("should not contain nav content, got: %q", result)
	}
}

func TestApplySelector_NoMatch(t *testing.T) {
	html := `<html><body><p>hello</p></body></html>`
	result, err := applySelector(html, ".nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result for no match, got: %q", result)
	}
}

func TestApplySelector_InvalidSelector(t *testing.T) {
	html := `<html><body><p>hello</p></body></html>`
	_, err := applySelector(html, "[invalid!!")
	if err == nil || !strings.Contains(err.Error(), "invalid CSS selector") {
		t.Fatalf("expected invalid selector error, got: %v", err)
	}
}

// --- HTML to markdown conversion ---

func TestHTMLToMarkdown_HeadingsAndParagraphs(t *testing.T) {
	input := `<h1>Title</h1><p>A paragraph.</p><h2>Subtitle</h2><p>More text.</p>`
	md, err := htmltomarkdown.ConvertString(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(md, "# Title") {
		t.Errorf("expected '# Title' in markdown, got: %q", md)
	}
	if !strings.Contains(md, "A paragraph.") {
		t.Errorf("expected paragraph text in markdown, got: %q", md)
	}
	if !strings.Contains(md, "## Subtitle") {
		t.Errorf("expected '## Subtitle' in markdown, got: %q", md)
	}
}

// --- Selector + markdown pipeline ---

func TestSelectorPlusMarkdown(t *testing.T) {
	input := `<html><body><nav><a href="/">Home</a></nav><main><h1>Article</h1><p>Body text.</p></main></body></html>`
	selected, err := applySelector(input, "main")
	if err != nil {
		t.Fatalf("selector error: %v", err)
	}
	md, err := htmltomarkdown.ConvertString(selected)
	if err != nil {
		t.Fatalf("conversion error: %v", err)
	}
	if !strings.Contains(md, "# Article") {
		t.Errorf("expected heading in markdown, got: %q", md)
	}
	if !strings.Contains(md, "Body text.") {
		t.Errorf("expected body text in markdown, got: %q", md)
	}
	if strings.Contains(md, "Home") {
		t.Errorf("nav content should be excluded, got: %q", md)
	}
}

// --- End-to-end with httptest ---

// testTransport returns an http.Client whose transport routes ALL requests
// to the given httptest server, regardless of the URL host. This lets us
// use a public-looking hostname (which passes the SSRF check) while the
// actual TCP connection goes to the local httptest server.
func testClientFor(srv *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Always dial the httptest server instead of the requested addr.
				return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= fetchMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
			}
			return nil
		},
	}
}

func TestFetchURL_EndToEnd_HTMLServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h1>Hello</h1><p>World</p></body></html>`)
	}))
	defer srv.Close()

	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	exe.httpClient = testClientFor(srv)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/page"})
	result, err := exe.fetchURL(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "# Hello") {
		t.Errorf("expected markdown heading, got: %q", result)
	}
	if !strings.Contains(result, "World") {
		t.Errorf("expected body text, got: %q", result)
	}
}

func TestFetchURL_RedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always redirect back to ourselves, creating an infinite loop.
		http.Redirect(w, r, "http://example.com/loop", http.StatusFound)
	}))
	defer srv.Close()

	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	exe.httpClient = testClientFor(srv)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/start"})
	_, err := exe.fetchURL(context.Background(), string(args))
	if err == nil {
		t.Fatal("expected redirect error, got nil")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected error mentioning redirect, got: %v", err)
	}
}

func TestFetchURL_NonHTMLPassthrough(t *testing.T) {
	payload := `{"status":"ok","count":42}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	exe.httpClient = testClientFor(srv)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/api"})
	result, err := exe.fetchURL(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != payload {
		t.Fatalf("expected JSON passthrough, got: %q", result)
	}
}

func TestFetchURL_RawHTMLPassthrough(t *testing.T) {
	rawHTML := `<html><body><h1>Title</h1><p>Paragraph</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, rawHTML)
	}))
	defer srv.Close()

	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	exe.httpClient = testClientFor(srv)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/page", "raw": true})
	result, err := exe.fetchURL(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// raw=true should return HTML as-is, not converted to markdown.
	if !strings.Contains(result, "<h1>Title</h1>") {
		t.Fatalf("expected raw HTML, got: %q", result)
	}
	if strings.Contains(result, "# Title") {
		t.Fatalf("raw mode should not convert to markdown, got: %q", result)
	}
}

func TestFetchURL_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<html><body><h1>Not Found</h1></body></html>`)
	}))
	defer srv.Close()

	exe, _ := newTestExecutor(t, &mockAuditLogger{})
	exe.httpClient = testClientFor(srv)

	args, _ := json.Marshal(map[string]any{"url": "http://example.com/missing"})
	result, err := exe.fetchURL(context.Background(), string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "HTTP 404") {
		t.Fatalf("expected output starting with 'HTTP 404', got: %q", result)
	}
}
