package provider

import (
	"context"
	"testing"
)

func TestMockProvider(t *testing.T) {
	mock := NewMock(
		Response{Content: "First response", Usage: Usage{InputTokens: 100, OutputTokens: 50}},
		Response{Content: "Second response"},
	)

	resp, err := mock.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "test", nil)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "First response" {
		t.Errorf("expected 'First response', got %q", resp.Content)
	}
	if resp.Usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", resp.Usage.InputTokens)
	}

	resp, _ = mock.Chat(context.Background(), []Message{{Role: "user", Content: "World"}}, nil, "test", nil)
	if resp.Content != "Second response" {
		t.Errorf("expected 'Second response', got %q", resp.Content)
	}

	// Beyond canned responses, returns default
	resp, _ = mock.Chat(context.Background(), []Message{{Role: "user", Content: "Extra"}}, nil, "test", nil)
	if resp.Content != "Mock response" {
		t.Errorf("expected 'Mock response', got %q", resp.Content)
	}

	if n := mock.CallCount(); n != 3 {
		t.Errorf("expected 3 calls recorded, got %d", n)
	}
}

func TestMockProviderName(t *testing.T) {
	mock := NewMock()
	if mock.Name() != "mock" {
		t.Errorf("expected 'mock', got %q", mock.Name())
	}
}

func TestMockWithToolCalls(t *testing.T) {
	mock := NewMock(Response{
		Content: "",
		ToolCalls: []ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "read_file",
					Arguments: `{"path": "/tmp/test.txt"}`,
				},
			},
		},
	})

	resp, _ := mock.Chat(context.Background(), nil, nil, "test", nil)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected 'read_file', got %q", resp.ToolCalls[0].Function.Name)
	}
}
