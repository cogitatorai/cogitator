package provider

import (
	"context"
	"fmt"
	"strings"
)

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ContentText extracts plain text from a Message's Content field.
// If Content is a string, returns it directly. If Content is a slice
// (multimodal), concatenates all text blocks.
func (m Message) ContentText() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if t, ok := block["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		if m.Content == nil {
			return ""
		}
		return fmt.Sprintf("%v", m.Content)
	}
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Usage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
}

type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage"`
}

type Provider interface {
	Chat(ctx context.Context, messages []Message, tools []Tool, model string, opts map[string]any) (*Response, error)
	Name() string
}

// Capabilities describes optional features a provider supports.
type Capabilities struct {
	StreamCancel bool // Provider supports mid-stream cancellation via context.
}

// CapabilityProvider is optionally implemented by providers that advertise
// their capabilities.
type CapabilityProvider interface {
	Capabilities() Capabilities
}

// Embedder generates vector embeddings for text. All OpenAI-compatible
// providers expose POST /v1/embeddings, so the OpenAI struct implements this.
type Embedder interface {
	Embed(ctx context.Context, texts []string, model string) ([][]float32, error)
}
