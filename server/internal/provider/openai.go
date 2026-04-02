package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Known provider base URLs. The user can also supply a custom base URL.
var knownBaseURLs = map[string]string{
	"openai":     "https://api.openai.com/v1",
	"anthropic":  "https://api.anthropic.com/v1",
	"ollama":     "http://localhost:11434/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"together":   "https://api.together.xyz/v1",
	"openrouter": "https://openrouter.ai/api/v1",
}

// keylessProviders lists providers that do not require an API key.
var keylessProviders = map[string]bool{
	"ollama": true,
}

// IsKeyless returns true if the named provider does not require an API key.
func IsKeyless(name string) bool {
	return keylessProviders[name]
}

// OpenAI implements the Provider interface using the OpenAI Chat Completions API.
// It works with any OpenAI-compatible endpoint (OpenAI, Groq, Together, Ollama, OpenRouter, etc.).
type OpenAI struct {
	apiKey       string
	baseURL      string
	name         string
	client       *http.Client
	ExtraHeaders map[string]string
}

// NewOpenAI creates a provider for the given provider name.
// If the name matches a known provider, its base URL is used automatically.
// Otherwise name is treated as a base URL.
func NewOpenAI(name, apiKey string) *OpenAI {
	base, ok := knownBaseURLs[name]
	if !ok {
		base = name
	}
	return &OpenAI{
		apiKey:  apiKey,
		baseURL: base,
		name:    name,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OpenAI) Name() string { return o.name }

var streamCancelProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
}

func (o *OpenAI) Capabilities() Capabilities {
	return Capabilities{
		StreamCancel: streamCancelProviders[o.name],
	}
}

func (o *OpenAI) Chat(ctx context.Context, messages []Message, tools []Tool, model string, opts map[string]any) (*Response, error) {
	body := map[string]any{
		"model":    model,
		"messages": messages,
	}
	if len(tools) > 0 {
		oaiTools := make([]map[string]any, len(tools))
		for i, t := range tools {
			oaiTools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			}
		}
		body["tools"] = oaiTools
	}
	for k, v := range opts {
		body[k] = v
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var respBody []byte
	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if o.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+o.apiKey)
		}
		for k, v := range o.ExtraHeaders {
			req.Header.Set(k, v)
		}

		var resp *http.Response
		resp, err = o.client.Do(req)
		if err != nil {
			// Context cancelled by caller: not retryable.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("http request: %w", err)
			}
			// Client timeout or network error: retryable.
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, fmt.Errorf("http request: %w", err)
				}
			}
			return nil, fmt.Errorf("http request (after %d retries): %w", maxRetries, err)
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		// 429 (rate limited) or 5xx (server error): retryable.
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				// Respect Retry-After header when present.
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if n, parseErr := strconv.Atoi(ra); parseErr == nil && n > 0 && n <= 60 {
						backoff = time.Duration(n) * time.Second
					}
				}
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, fmt.Errorf("API %d (retry aborted): %s", resp.StatusCode, string(respBody))
				}
			}
			return nil, fmt.Errorf("API %d (after %d retries): %s", resp.StatusCode, maxRetries, string(respBody))
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API %d: %s", resp.StatusCode, string(respBody))
		}

		break // success
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	choice := oaiResp.Choices[0]
	result := &Response{
		Content: choice.Message.Content,
		Usage: Usage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return result, nil
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIMessage struct {
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (o *OpenAI) Embed(ctx context.Context, texts []string, model string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(map[string]any{
		"model": model,
		"input": texts,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp openAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	sort.Slice(embResp.Data, func(i, j int) bool {
		return embResp.Data[i].Index < embResp.Data[j].Index
	})

	result := make([][]float32, len(embResp.Data))
	for i, d := range embResp.Data {
		result[i] = d.Embedding
	}
	return result, nil
}
