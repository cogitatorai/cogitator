package connector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/itchyny/gojq"
)

// RESTExecutor executes HTTP requests defined by connector tool manifests
// and maps responses using jq expressions.
type RESTExecutor struct{}

// NewRESTExecutor creates a new REST executor.
func NewRESTExecutor() *RESTExecutor {
	return &RESTExecutor{}
}

// Execute runs a tool's HTTP request with the given arguments and returns
// the JSON result as a string.
func (e *RESTExecutor) Execute(client *http.Client, tool ToolManifest, args map[string]any) (string, error) {
	// Render the URL template.
	reqURL, err := renderTemplate(tool.Request.URL, args)
	if err != nil {
		return "", fmt.Errorf("rendering URL: %w", err)
	}

	// Build query parameters.
	if len(tool.Request.Query) > 0 {
		u, err := url.Parse(reqURL)
		if err != nil {
			return "", fmt.Errorf("parsing URL: %w", err)
		}
		q := u.Query()
		for k, v := range tool.Request.Query {
			rendered, err := renderTemplate(v, args)
			if err != nil {
				return "", fmt.Errorf("rendering query %q: %w", k, err)
			}
			if rendered != "" {
				q.Set(k, rendered)
			}
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	// Make the HTTP request.
	method := tool.Request.Method
	if method == "" {
		method = "GET"
	}
	body, err := doRequest(client, method, reqURL)
	if err != nil {
		return "", err
	}

	// If fetch_each is defined, do the list+fetch pattern.
	if tool.FetchEach != nil {
		return e.executeFetchEach(client, tool, body)
	}

	// Map the response.
	return mapResponse(body, tool.Response)
}

// executeFetchEach implements the list+fetch pattern:
// 1. Extract IDs from the list response using id_path
// 2. Fetch each item by ID
// 3. Map each item's response
func (e *RESTExecutor) executeFetchEach(client *http.Client, tool ToolManifest, listBody any) (string, error) {
	fe := tool.FetchEach

	// Extract IDs from the list response.
	ids, err := queryJQ(listBody, fe.IDPath)
	if err != nil {
		return "", fmt.Errorf("extracting IDs: %w", err)
	}

	var results []map[string]any
	for _, idVal := range ids {
		idStr := fmt.Sprintf("%v", idVal)

		// Render the fetch URL with the ID.
		fetchArgs := map[string]any{"id": idStr}
		fetchURL, err := renderTemplate(fe.Request.URL, fetchArgs)
		if err != nil {
			continue
		}

		// Build query params for fetch request.
		if len(fe.Request.Query) > 0 {
			u, err := url.Parse(fetchURL)
			if err != nil {
				continue
			}
			q := u.Query()
			for k, v := range fe.Request.Query {
				rendered, _ := renderTemplate(v, fetchArgs)
				if rendered != "" {
					q.Set(k, rendered)
				}
			}
			u.RawQuery = q.Encode()
			fetchURL = u.String()
		}

		method := fe.Request.Method
		if method == "" {
			method = "GET"
		}
		itemBody, err := doRequest(client, method, fetchURL)
		if err != nil {
			continue
		}

		mapped, err := mapFields(itemBody, fe.Response.Fields)
		if err != nil {
			continue
		}
		results = append(results, mapped)
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// doRequest makes an HTTP request and returns the parsed JSON body.
func doRequest(client *http.Client, method, reqURL string) (any, error) {
	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	slog.Debug("connector HTTP request", "method", method, "url", reqURL)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	slog.Debug("connector HTTP response", "status", resp.StatusCode, "bodyLen", len(respBody), "body", truncate(string(respBody), 2000))

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncate(string(respBody), 1000))
	}

	var parsed any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parsing JSON response: %w", err)
	}
	return parsed, nil
}

// mapResponse applies root extraction and field mapping to a response body.
func mapResponse(body any, resp ResponseDef) (string, error) {
	// Apply root jq expression if present.
	if resp.Root != "" {
		items, err := queryJQ(body, resp.Root)
		if err != nil {
			return "", fmt.Errorf("extracting root: %w", err)
		}
		// Unwrap: if the root returned a single array, iterate its elements.
		items = flattenArrayResults(items)
		// If fields are defined, map each item.
		if len(resp.Fields) > 0 {
			results := make([]map[string]any, 0, len(items))
			for _, item := range items {
				mapped, err := mapFields(item, resp.Fields)
				if err != nil {
					continue
				}
				results = append(results, mapped)
			}
			data, _ := json.MarshalIndent(results, "", "  ")
			return string(data), nil
		}
		// No field mapping; return raw items.
		if len(items) == 0 {
			return "[]", nil
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		return string(data), nil
	}

	// No root; map the whole body if fields are defined.
	if len(resp.Fields) > 0 {
		mapped, err := mapFields(body, resp.Fields)
		if err != nil {
			return "", err
		}
		data, _ := json.MarshalIndent(mapped, "", "  ")
		return string(data), nil
	}

	// No mapping at all; return the raw response.
	data, _ := json.MarshalIndent(body, "", "  ")
	return string(data), nil
}

// mapFields extracts named fields from a JSON value using jq expressions.
func mapFields(body any, fields map[string]string) (map[string]any, error) {
	result := make(map[string]any, len(fields))
	for name, expr := range fields {
		vals, err := queryJQ(body, expr)
		if err != nil || len(vals) == 0 {
			result[name] = nil
			continue
		}
		result[name] = vals[0]
	}
	return result, nil
}

// queryJQ runs a jq expression against a JSON value and returns all results.
func queryJQ(input any, expr string) ([]any, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("parsing jq %q: %w", expr, err)
	}
	var results []any
	iter := query.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, err
		}
		results = append(results, v)
	}
	return results, nil
}

// renderTemplate renders a Go text/template string with the given data.
// Returns empty string if the template result is "<no value>" or empty.
func renderTemplate(tmpl string, data map[string]any) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}
	funcMap := template.FuncMap{
		"default": func(def any, val any) any {
			if val == nil || val == "" {
				return def
			}
			return val
		},
		// localtime converts a date string (YYYY-MM-DD), time string (HH:MM:SS),
		// and IANA timezone name to an RFC3339 timestamp in that timezone.
		// Example: {{localtime "2026-03-06" "00:00:00" "Europe/Paris"}} -> "2026-03-06T00:00:00+01:00"
		"localtime": func(date, timeStr, tz string) string {
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return date + "T" + timeStr + "Z"
			}
			t, err := time.ParseInLocation("2006-01-02T15:04:05", date+"T"+timeStr, loc)
			if err != nil {
				return date + "T" + timeStr + "Z"
			}
			return t.Format(time.RFC3339)
		},
	}
	t, err := template.New("").Funcs(funcMap).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	result := buf.String()
	if result == "<no value>" {
		return "", nil
	}
	return result, nil
}

// flattenArrayResults unwraps jq results: if the result is a single array value,
// return its elements individually so field mapping works per-item.
func flattenArrayResults(results []any) []any {
	if len(results) == 1 {
		if arr, ok := results[0].([]any); ok {
			return arr
		}
	}
	return results
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
