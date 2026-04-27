package provider

import (
	"context"
	"sync"
)

type MockProvider struct {
	Responses     []Response
	StreamCancel  bool
	EmbedResponse [][]float32

	mu            sync.Mutex
	Calls         [][]Message
	RecordedTools [][]Tool // parallel to Calls: tools passed to each Chat invocation
	callIdx       int
}

func NewMock(responses ...Response) *MockProvider {
	return &MockProvider{Responses: responses}
}

func (m *MockProvider) Chat(ctx context.Context, messages []Message, tools []Tool, _ string, _ map[string]any) (*Response, error) {
	// When StreamCancel is enabled, honour context cancellation like a real
	// streaming provider would.
	if m.StreamCancel {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, messages)
	m.RecordedTools = append(m.RecordedTools, tools)

	if m.callIdx < len(m.Responses) {
		resp := m.Responses[m.callIdx]
		m.callIdx++
		return &resp, nil
	}

	return &Response{
		Content: "Mock response",
		Usage:   Usage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

// CallCount returns the number of Chat calls made so far (thread-safe).
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// GetCalls returns a deep copy of the recorded calls (thread-safe).
// Each inner []Message slice is copied so the caller can inspect messages
// without holding the lock.
func (m *MockProvider) GetCalls() [][]Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([][]Message, len(m.Calls))
	for i, msgs := range m.Calls {
		inner := make([]Message, len(msgs))
		copy(inner, msgs)
		cp[i] = inner
	}
	return cp
}

// GetRecordedTools returns a shallow copy of the tools slice recorded per call (thread-safe).
func (m *MockProvider) GetRecordedTools() [][]Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]Tool, len(m.RecordedTools))
	copy(cp, m.RecordedTools)
	return cp
}

func (m *MockProvider) Capabilities() Capabilities {
	return Capabilities{StreamCancel: m.StreamCancel}
}

func (m *MockProvider) Name() string {
	return "mock"
}

func (m *MockProvider) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	if m.EmbedResponse != nil {
		return m.EmbedResponse, nil
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{0.1, 0.2, 0.3}
	}
	return result, nil
}
