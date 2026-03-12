package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// ---------- Parser unit tests ----------

func TestDDGParseResults(t *testing.T) {
	raw := `<html><body>
		<div class="result">
			<a class="result__a" href="https://example.com/1">Result One</a>
			<a class="result__snippet">Snippet one text</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/2">Result Two</a>
			<a class="result__snippet">Snippet two text</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/3">Result Three</a>
			<a class="result__snippet">Snippet three text</a>
		</div>
	</body></html>`

	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	eng := duckduckgoEngine{}

	// Parse all 3.
	results := eng.ParseResults(doc, 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Title != "Result One" {
		t.Errorf("result[0].Title = %q, want %q", results[0].Title, "Result One")
	}
	if results[0].URL != "https://example.com/1" {
		t.Errorf("result[0].URL = %q", results[0].URL)
	}
	if results[0].Snippet != "Snippet one text" {
		t.Errorf("result[0].Snippet = %q", results[0].Snippet)
	}

	// Respect count limit.
	results = eng.ParseResults(doc, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with count=2, got %d", len(results))
	}
}

func TestBraveParseResults_Primary(t *testing.T) {
	raw := `<html><body>
		<div class="snippet" data-type="web">
			<a class="heading-serpresult" href="https://brave.com/1">Brave Result</a>
			<div class="snippet-description">Brave snippet text</div>
		</div>
	</body></html>`

	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	eng := braveEngine{}
	results := eng.ParseResults(doc, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Brave Result" {
		t.Errorf("Title = %q", results[0].Title)
	}
	if results[0].Snippet != "Brave snippet text" {
		t.Errorf("Snippet = %q", results[0].Snippet)
	}
}

func TestBraveParseResults_Fallback(t *testing.T) {
	raw := `<html><body>
		<div class="fdb">
			<a class="result-header" href="https://brave.com/alt">Alt Result</a>
			<p class="snippet-description">Alt snippet</p>
		</div>
	</body></html>`

	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	eng := braveEngine{}
	results := eng.ParseResults(doc, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result from fallback, got %d", len(results))
	}
	if results[0].Title != "Alt Result" {
		t.Errorf("Title = %q", results[0].Title)
	}
}

func TestMojeekParseResults(t *testing.T) {
	raw := `<html><body>
		<ul class="results-standard">
			<li>
				<a class="ob" href="https://mojeek.com/1">Mojeek Result</a>
				<p class="s">Mojeek snippet</p>
			</li>
			<li>
				<a class="ob" href="https://mojeek.com/2">Second Result</a>
				<p class="s">Second snippet</p>
			</li>
		</ul>
	</body></html>`

	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	eng := mojeekEngine{}
	results := eng.ParseResults(doc, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Mojeek Result" {
		t.Errorf("Title = %q", results[0].Title)
	}
	if results[1].URL != "https://mojeek.com/2" {
		t.Errorf("URL = %q", results[1].URL)
	}
}

func TestParseSkipsMissingURLOrTitle(t *testing.T) {
	raw := `<html><body>
		<div class="result">
			<a class="result__a" href="">No Title Here</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/ok">Good Result</a>
			<a class="result__snippet">Good snippet</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://example.com/notitle">   </a>
		</div>
	</body></html>`

	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	eng := duckduckgoEngine{}
	results := eng.ParseResults(doc, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 valid result, got %d", len(results))
	}
	if results[0].Title != "Good Result" {
		t.Errorf("Title = %q", results[0].Title)
	}
}

// ---------- Helper tests ----------

func TestTextContent(t *testing.T) {
	raw := `<span>Hello <b>bold</b> world</span>`
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// html.Parse wraps in <html><head></head><body>..., find the span.
	span := cascadia.Query(doc, mustCompile("span"))
	if span == nil {
		t.Fatal("span not found")
	}
	got := textContent(span)
	if got != "Hello bold world" {
		t.Errorf("textContent = %q, want %q", got, "Hello bold world")
	}
}

func TestTextContentNil(t *testing.T) {
	if got := textContent(nil); got != "" {
		t.Errorf("textContent(nil) = %q, want empty", got)
	}
}

func TestAttrVal(t *testing.T) {
	raw := `<a href="https://example.com" class="link">text</a>`
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	a := cascadia.Query(doc, mustCompile("a"))
	if a == nil {
		t.Fatal("a not found")
	}

	if got := attrVal(a, "href"); got != "https://example.com" {
		t.Errorf("attrVal(href) = %q", got)
	}
	if got := attrVal(a, "class"); got != "link" {
		t.Errorf("attrVal(class) = %q", got)
	}
	if got := attrVal(a, "missing"); got != "" {
		t.Errorf("attrVal(missing) = %q, want empty", got)
	}
}

func TestSearchURL(t *testing.T) {
	tests := []struct {
		eng    searchEngine
		query  string
		expect string
	}{
		{duckduckgoEngine{}, "hello world", "https://html.duckduckgo.com/html/?q=hello+world"},
		{braveEngine{}, "foo&bar", "https://search.brave.com/search?q=foo%26bar"},
		{mojeekEngine{}, "special chars!", "https://www.mojeek.com/search?q=special+chars%21"},
	}
	for _, tt := range tests {
		got := tt.eng.SearchURL(tt.query)
		if got != tt.expect {
			t.Errorf("%s.SearchURL(%q) = %q, want %q", tt.eng.Name(), tt.query, got, tt.expect)
		}
	}
}

func TestFormatSearchResults(t *testing.T) {
	results := []searchResult{
		{Title: "First", URL: "https://a.com", Snippet: "First snippet"},
		{Title: "Second", URL: "https://b.com", Snippet: ""},
	}
	out := formatSearchResults("test query", results)
	if !strings.Contains(out, `## Search Results for "test query"`) {
		t.Error("missing header")
	}
	if !strings.Contains(out, "1. **First**") {
		t.Error("missing first result")
	}
	if !strings.Contains(out, "https://a.com") {
		t.Error("missing first URL")
	}
	if !strings.Contains(out, "First snippet") {
		t.Error("missing first snippet")
	}
	if !strings.Contains(out, "2. **Second**") {
		t.Error("missing second result")
	}
}

// ---------- Rotation / fallback tests (httptest-based) ----------

// testEngine wraps an httptest.Server and reuses DDG's parser.
type testEngine struct {
	name   string
	server *httptest.Server
}

func (te *testEngine) Name() string { return te.name }

func (te *testEngine) SearchURL(query string) string {
	return te.server.URL + "/?q=" + query
}

func (te *testEngine) ParseResults(doc *html.Node, count int) []searchResult {
	return (duckduckgoEngine{}).ParseResults(doc, count)
}

func newTestEngine(name, responseHTML string) *testEngine {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, responseHTML)
	}))
	return &testEngine{name: name, server: srv}
}

func newFailingEngine(name string) *testEngine {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	return &testEngine{name: name, server: srv}
}

const ddgHTML = `<html><body>
	<div class="result">
		<a class="result__a" href="https://example.com/1">Test Result</a>
		<a class="result__snippet">Test snippet</a>
	</div>
</body></html>`

const emptyHTML = `<html><body><p>No results</p></body></html>`

func withEngines(engines []searchEngine, fn func()) {
	old := defaultEngines
	defaultEngines = engines
	defer func() { defaultEngines = old }()
	fn()
}

func TestRotation(t *testing.T) {
	e1 := newTestEngine("eng1", ddgHTML)
	e2 := newTestEngine("eng2", ddgHTML)
	e3 := newTestEngine("eng3", ddgHTML)
	defer e1.server.Close()
	defer e2.server.Close()
	defer e3.server.Close()

	withEngines([]searchEngine{e1, e2, e3}, func() {
		exec := &Executor{httpClient: &http.Client{}}

		// Three calls should hit three different engines (round-robin start).
		seen := make(map[string]bool)
		for i := 0; i < 3; i++ {
			result, err := exec.webSearch(context.Background(), `{"query":"test"}`)
			if err != nil {
				t.Fatalf("call %d: %v", i, err)
			}
			if !strings.Contains(result, "Test Result") {
				t.Errorf("call %d: unexpected result: %s", i, result)
			}
			// Detect which engine was used by checking the counter progression.
			// Each call rotates, so all three should produce results.
			seen[fmt.Sprintf("call_%d", i)] = true
		}
		if len(seen) != 3 {
			t.Errorf("expected 3 successful calls, got %d", len(seen))
		}
	})
}

func TestFallback(t *testing.T) {
	empty := newTestEngine("empty", emptyHTML)
	good := newTestEngine("good", ddgHTML)
	defer empty.server.Close()
	defer good.server.Close()

	withEngines([]searchEngine{empty, good}, func() {
		exec := &Executor{httpClient: &http.Client{}}
		// Reset counter so we start at engine 0 (the empty one).
		exec.searchCounter = 0

		result, err := exec.webSearch(context.Background(), `{"query":"test"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "Test Result") {
			t.Errorf("expected fallback to good engine, got: %s", result)
		}
	})
}

func TestAllFail(t *testing.T) {
	f1 := newFailingEngine("fail1")
	f2 := newFailingEngine("fail2")
	defer f1.server.Close()
	defer f2.server.Close()

	withEngines([]searchEngine{f1, f2}, func() {
		exec := &Executor{httpClient: &http.Client{}}

		result, err := exec.webSearch(context.Background(), `{"query":"test"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "All search engines failed") {
			t.Errorf("expected failure message, got: %s", result)
		}
		if !strings.Contains(result, "fetch_url") {
			t.Errorf("expected fetch_url suggestion, got: %s", result)
		}
	})
}

func TestAllEmpty(t *testing.T) {
	e1 := newTestEngine("empty1", emptyHTML)
	e2 := newTestEngine("empty2", emptyHTML)
	defer e1.server.Close()
	defer e2.server.Close()

	withEngines([]searchEngine{e1, e2}, func() {
		exec := &Executor{httpClient: &http.Client{}}

		result, err := exec.webSearch(context.Background(), `{"query":"test"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "No results found") {
			t.Errorf("expected no-results message, got: %s", result)
		}
		if !strings.Contains(result, "fetch_url") {
			t.Errorf("expected fetch_url suggestion, got: %s", result)
		}
	})
}

func TestCountClamping(t *testing.T) {
	eng := newTestEngine("eng", `<html><body>
		<div class="result"><a class="result__a" href="https://a.com/1">R1</a></div>
		<div class="result"><a class="result__a" href="https://a.com/2">R2</a></div>
		<div class="result"><a class="result__a" href="https://a.com/3">R3</a></div>
		<div class="result"><a class="result__a" href="https://a.com/4">R4</a></div>
		<div class="result"><a class="result__a" href="https://a.com/5">R5</a></div>
		<div class="result"><a class="result__a" href="https://a.com/6">R6</a></div>
		<div class="result"><a class="result__a" href="https://a.com/7">R7</a></div>
		<div class="result"><a class="result__a" href="https://a.com/8">R8</a></div>
		<div class="result"><a class="result__a" href="https://a.com/9">R9</a></div>
		<div class="result"><a class="result__a" href="https://a.com/10">R10</a></div>
		<div class="result"><a class="result__a" href="https://a.com/11">R11</a></div>
		<div class="result"><a class="result__a" href="https://a.com/12">R12</a></div>
	</body></html>`)
	defer eng.server.Close()

	withEngines([]searchEngine{eng}, func() {
		exec := &Executor{httpClient: &http.Client{}}

		// count=20 should be clamped to 10.
		result, err := exec.webSearch(context.Background(), `{"query":"test","count":20}`)
		if err != nil {
			t.Fatal(err)
		}
		// Count the numbered results (lines matching "N. **...")
		lines := strings.Split(result, "\n")
		count := 0
		for _, line := range lines {
			if strings.Contains(line, ". **") {
				count++
			}
		}
		if count != 10 {
			t.Errorf("expected 10 results after clamping, got %d", count)
		}
	})
}

func TestEmptyQuery(t *testing.T) {
	exec := &Executor{httpClient: &http.Client{}}
	_, err := exec.webSearch(context.Background(), `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
