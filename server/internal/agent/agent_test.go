package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/tools"
)

func setupTestAgent(t *testing.T, responses ...provider.Response) (*Agent, *provider.MockProvider, *session.Store, *bus.Bus) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte("Be helpful."), 0o644)

	mock := provider.NewMock(responses...)
	store := session.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	agent := New(Config{
		Provider:       mock,
		Sessions:       store,
		ContextBuilder: NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "test-model",
	})

	return agent, mock, store, eventBus
}

func TestChatBasicResponse(t *testing.T) {
	agent, mock, store, _ := setupTestAgent(t,
		provider.Response{Content: "Hello there!", Usage: provider.Usage{InputTokens: 50, OutputTokens: 20}},
	)

	resp, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "test-session",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "Hi",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	if resp.Content != "Hello there!" {
		t.Errorf("expected 'Hello there!', got %q", resp.Content)
	}
	if resp.Usage.InputTokens != 50 {
		t.Errorf("expected 50 input tokens, got %d", resp.Usage.InputTokens)
	}

	// Verify messages were persisted
	msgs, err := store.GetMessages("test-session", 0)
	if err != nil {
		t.Fatalf("GetMessages error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hi" {
		t.Errorf("expected user message 'Hi', got %q %q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hello there!" {
		t.Errorf("expected assistant message, got %q %q", msgs[1].Role, msgs[1].Content)
	}

	// Verify provider was called at least once for the chat (a background
	// goroutine may add a second call for title generation).
	if n := mock.CallCount(); n < 1 {
		t.Errorf("expected at least 1 provider call, got %d", n)
	}
}

func TestChatMessageSuffix(t *testing.T) {
	agent, mock, store, _ := setupTestAgent(t,
		provider.Response{Content: "Short answer."},
	)

	_, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey:    "suffix-test",
		Channel:       "web",
		ChatID:        "chat1",
		Message:       "What is Go?",
		MessageSuffix: "[Be concise]",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	// Verify the suffix was sent to the LLM.
	calls := mock.GetCalls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 provider call")
	}
	firstCall := calls[0]
	// Find the last user message in the call.
	var userContent string
	for _, m := range firstCall {
		if m.Role == "user" {
			userContent = m.ContentText()
		}
	}
	if !strings.Contains(userContent, "[Be concise]") {
		t.Errorf("expected suffix in LLM message, got %q", userContent)
	}
	if !strings.Contains(userContent, "What is Go?") {
		t.Errorf("expected original message in LLM message, got %q", userContent)
	}

	// Verify the suffix was NOT stored in session history.
	msgs, err := store.GetMessages("suffix-test", 0)
	if err != nil {
		t.Fatalf("GetMessages error: %v", err)
	}
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 stored message")
	}
	storedUser := msgs[0]
	if storedUser.Content != "What is Go?" {
		t.Errorf("stored message should be just the transcription, got %q", storedUser.Content)
	}
	if strings.Contains(storedUser.Content, "[Be concise]") {
		t.Error("suffix should not be in stored message")
	}
}

func TestChatEmitsEvents(t *testing.T) {
	agent, _, _, eventBus := setupTestAgent(t,
		provider.Response{Content: "Response"},
	)

	received := eventBus.Subscribe(bus.MessageReceived)
	responded := eventBus.Subscribe(bus.MessageResponded)

	_, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "evt-session",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "Hello",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	select {
	case evt := <-received:
		if evt.Payload["session_key"] != "evt-session" {
			t.Errorf("expected session_key 'evt-session', got %v", evt.Payload["session_key"])
		}
	default:
		t.Error("expected MessageReceived event")
	}

	select {
	case evt := <-responded:
		if evt.Payload["session_key"] != "evt-session" {
			t.Errorf("expected session_key 'evt-session', got %v", evt.Payload["session_key"])
		}
	default:
		t.Error("expected MessageResponded event")
	}
}

type mockToolExecutor struct {
	results map[string]string
	calls   []string
}

func (m *mockToolExecutor) Execute(_ context.Context, name string, _ string) (string, error) {
	m.calls = append(m.calls, name)
	if result, ok := m.results[name]; ok {
		return result, nil
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

func (m *mockToolExecutor) ResolveToolNames(names []string) tools.ResolvedTools {
	return tools.Resolve(names, nil)
}

func TestChatWithToolCalls(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	// First response: tool call. Second response: final answer.
	mock := provider.NewMock(
		provider.Response{
			ToolCalls: []provider.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: provider.FunctionCall{
						Name:      "read_file",
						Arguments: `{"path": "/tmp/test.txt"}`,
					},
				},
			},
			Usage: provider.Usage{InputTokens: 30, OutputTokens: 10},
		},
		provider.Response{
			Content: "The file contains: hello world",
			Usage:   provider.Usage{InputTokens: 50, OutputTokens: 25},
		},
	)

	executor := &mockToolExecutor{
		results: map[string]string{
			"read_file": "hello world",
		},
	}

	store := session.NewStore(db)
	eventBus := bus.New()
	defer eventBus.Close()

	agent := New(Config{
		Provider:       mock,
		Sessions:       store,
		ContextBuilder: NewContextBuilder(profilePath),
		ToolExecutor:   executor,
		EventBus:       eventBus,
		Model:          "test",
	})

	resp, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "tool-session",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "Read the file",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	if resp.Content != "The file contains: hello world" {
		t.Errorf("unexpected content: %q", resp.Content)
	}

	// Usage should be accumulated from both rounds
	if resp.Usage.InputTokens != 80 {
		t.Errorf("expected 80 total input tokens, got %d", resp.Usage.InputTokens)
	}

	// Tool executor should have been called
	if len(executor.calls) != 1 || executor.calls[0] != "read_file" {
		t.Errorf("expected read_file call, got %v", executor.calls)
	}

	// Provider should have been called twice (tool call + final)
	if n := mock.CallCount(); n != 2 {
		t.Errorf("expected 2 provider calls, got %d", n)
	}

	// Second call should include the tool result message
	calls := mock.GetCalls()
	secondCallMsgs := calls[1]
	var foundToolMsg bool
	for _, m := range secondCallMsgs {
		if m.Role == "tool" && m.ToolCallID == "call_1" {
			foundToolMsg = true
			if m.Content != "hello world" {
				t.Errorf("expected tool result 'hello world', got %q", m.Content)
			}
		}
	}
	if !foundToolMsg {
		t.Error("expected tool result message in second provider call")
	}

	// All messages should be persisted
	msgs, err := store.GetMessages("tool-session", 0)
	if err != nil {
		t.Fatalf("GetMessages error: %v", err)
	}
	// user + assistant(tool call) + tool result + assistant(final)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 persisted messages, got %d", len(msgs))
	}
}

func TestChatMaxToolRoundsExceeded(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	// Always returns a tool call, never a final response
	alwaysToolCall := provider.Response{
		ToolCalls: []provider.ToolCall{
			{ID: "call_loop", Type: "function", Function: provider.FunctionCall{Name: "noop", Arguments: "{}"}},
		},
	}
	mock := provider.NewMock(alwaysToolCall, alwaysToolCall, alwaysToolCall, alwaysToolCall)

	executor := &mockToolExecutor{
		results: map[string]string{"noop": "ok"},
	}

	agent := New(Config{
		Provider:       mock,
		Sessions:       session.NewStore(db),
		ContextBuilder: NewContextBuilder(profilePath),
		ToolExecutor:   executor,
		EventBus:       bus.New(),
		Model:          "test",
		MaxToolRounds:  2,
	})

	_, err = agent.Chat(context.Background(), ChatRequest{
		SessionKey: "loop-session",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "loop forever",
	})

	if err == nil {
		t.Fatal("expected error for exceeding max tool rounds")
	}
}

func TestChatWithoutSessionStore(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	mock := provider.NewMock(provider.Response{Content: "No store"})

	agent := New(Config{
		Provider:       mock,
		ContextBuilder: NewContextBuilder(profilePath),
		Model:          "test",
	})

	resp, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "no-store",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "Hello",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "No store" {
		t.Errorf("expected 'No store', got %q", resp.Content)
	}
}

func TestChatToolExecutorError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	mock := provider.NewMock(
		provider.Response{
			ToolCalls: []provider.ToolCall{
				{ID: "call_err", Type: "function", Function: provider.FunctionCall{Name: "bad_tool", Arguments: "{}"}},
			},
		},
		provider.Response{Content: "I see the error"},
	)

	executor := &mockToolExecutor{results: map[string]string{}}

	agent := New(Config{
		Provider:       mock,
		Sessions:       session.NewStore(db),
		ContextBuilder: NewContextBuilder(profilePath),
		ToolExecutor:   executor,
		EventBus:       bus.New(),
		Model:          "test",
	})

	resp, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "err-session",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "use bad tool",
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	// The error message should have been passed to the LLM as tool result
	calls := mock.GetCalls()
	secondCallMsgs := calls[1]
	var foundErrMsg bool
	for _, m := range secondCallMsgs {
		if m.Role == "tool" && m.ToolCallID == "call_err" {
			foundErrMsg = true
			if m.Content != "Error: unknown tool: bad_tool" {
				t.Errorf("unexpected error content: %q", m.Content)
			}
		}
	}
	if !foundErrMsg {
		t.Error("expected error message in tool result")
	}

	if resp.Content != "I see the error" {
		t.Errorf("expected 'I see the error', got %q", resp.Content)
	}
}

func TestChatNilProvider(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	agent := New(Config{
		ContextBuilder: NewContextBuilder(profilePath),
		Model:          "test",
	})

	_, err := agent.Chat(context.Background(), ChatRequest{
		SessionKey: "nil-provider",
		Channel:    "web",
		ChatID:     "chat1",
		Message:    "Hello",
	})
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
	if err.Error() != "no LLM provider configured" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTruncateToolResult(t *testing.T) {
	t.Run("under limit passes through unchanged", func(t *testing.T) {
		input := "short result"
		got := truncateToolResult(input, 100)
		if got != input {
			t.Errorf("expected unchanged output, got %q", got)
		}
	})

	t.Run("exact limit passes through unchanged", func(t *testing.T) {
		input := strings.Repeat("x", 100)
		got := truncateToolResult(input, 100)
		if got != input {
			t.Errorf("expected unchanged output, got len %d", len(got))
		}
	})

	t.Run("over limit is truncated with notice", func(t *testing.T) {
		input := strings.Repeat("a", 200)
		max := 100
		got := truncateToolResult(input, max)

		if !strings.HasPrefix(got, strings.Repeat("a", max)) {
			t.Error("expected truncated output to start with the first max bytes")
		}

		wantSuffix := "\n\n[output truncated: " + strconv.Itoa(len(input)) + " bytes, showing first " + strconv.Itoa(max) + "]"
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("expected suffix %q, got tail %q", wantSuffix, got[max:])
		}
	})
}

func TestCallProvider_StreamCancel_PropagatesContext(t *testing.T) {
	mock := provider.NewMock(provider.Response{Content: "ok"})
	mock.StreamCancel = true
	agent := New(Config{Provider: mock, Model: "m"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := agent.callProvider(ctx, mock, nil, nil, "m")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestCallProvider_NoStreamCancel_DetachAndDiscard(t *testing.T) {
	blocking := &blockingProvider{
		ch:   make(chan struct{}),
		resp: &provider.Response{Content: "discarded"},
	}
	agent := New(Config{Provider: blocking, Model: "m"})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := agent.callProvider(ctx, blocking, nil, nil, "m")
		done <- err
	}()

	// Cancel before the provider returns.
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Unblock the provider goroutine so it can clean up.
	close(blocking.ch)
}

// blockingProvider blocks on ch before returning. Does not implement CapabilityProvider.
type blockingProvider struct {
	ch   chan struct{}
	resp *provider.Response
}

func (b *blockingProvider) Chat(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ map[string]any) (*provider.Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.ch:
		return b.resp, nil
	}
}

func (b *blockingProvider) Name() string { return "blocking" }

type blockingToolExecutor struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *blockingToolExecutor) Execute(ctx context.Context, name, args string) (string, error) {
	b.cancel()
	<-ctx.Done()
	return "", ctx.Err()
}

func (b *blockingToolExecutor) ResolveToolNames(names []string) tools.ResolvedTools {
	return tools.ResolvedTools{}
}

func TestChat_CancelledDuringLLM_CleansUpMessages(t *testing.T) {
	blocking := &blockingProvider{
		ch:   make(chan struct{}),
		resp: &provider.Response{Content: "should be discarded"},
	}

	dir := t.TempDir()
	db, _ := database.Open(filepath.Join(dir, "test.db"))
	t.Cleanup(func() { db.Close() })

	store := session.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte("Be helpful."), 0o644)

	agent := New(Config{
		Provider:       blocking,
		Sessions:       store,
		ContextBuilder: NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "m",
	})

	ch := eventBus.Subscribe(bus.AgentCancelled)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := agent.Chat(ctx, ChatRequest{
			SessionKey: "cancel-test",
			Channel:    "web",
			Message:    "Hello",
		})
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errCh
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}

	msgs, _ := store.GetMessages("cancel-test", 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + system), got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("first message should be user 'Hello', got %q %q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "system" || msgs[1].Content != "[cancelled]" {
		t.Errorf("second message should be system '[cancelled]', got %q %q", msgs[1].Role, msgs[1].Content)
	}

	select {
	case evt := <-ch:
		if evt.Type != bus.AgentCancelled {
			t.Errorf("expected AgentCancelled, got %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for AgentCancelled event")
	}

	close(blocking.ch)
}

func TestChat_CancelledDuringToolExecution_CleansUpMessages(t *testing.T) {
	mock := provider.NewMock(
		provider.Response{
			Content: "",
			ToolCalls: []provider.ToolCall{{
				ID: "tc1", Type: "function",
				Function: provider.FunctionCall{Name: "slow_tool", Arguments: "{}"},
			}},
		},
	)

	dir := t.TempDir()
	db, _ := database.Open(filepath.Join(dir, "test.db"))
	t.Cleanup(func() { db.Close() })

	store := session.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte("Be helpful."), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	executor := &blockingToolExecutor{ctx: ctx, cancel: cancel}

	agent := New(Config{
		Provider:       mock,
		Sessions:       store,
		ContextBuilder: NewContextBuilder(profilePath),
		ToolExecutor:   executor,
		EventBus:       eventBus,
		Model:          "m",
	})

	_, err := agent.Chat(ctx, ChatRequest{
		SessionKey: "cancel-tool-test",
		Channel:    "web",
		Message:    "Do something",
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}

	msgs, _ := store.GetMessages("cancel-tool-test", 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + system), got %d", len(msgs))
	}
	if msgs[1].Role != "system" || msgs[1].Content != "[cancelled]" {
		t.Errorf("expected system '[cancelled]', got %q %q", msgs[1].Role, msgs[1].Content)
	}
}

// multiRoundProvider returns pre-set responses for the first N calls,
// then blocks on blockCh for subsequent calls.
type multiRoundProvider struct {
	mu        sync.Mutex
	responses []*provider.Response
	callIdx   int
	blockCh   chan struct{}
}

func (p *multiRoundProvider) Chat(ctx context.Context, _ []provider.Message, _ []provider.Tool, _ string, _ map[string]any) (*provider.Response, error) {
	p.mu.Lock()
	idx := p.callIdx
	p.callIdx++
	p.mu.Unlock()

	if idx < len(p.responses) {
		return p.responses[idx], nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.blockCh:
		return &provider.Response{Content: "should not reach here"}, nil
	}
}

func (p *multiRoundProvider) Name() string { return "multi-round" }

// simpleToolExecutor returns a fixed result for any tool.
type simpleToolExecutor struct {
	result string
}

func (e *simpleToolExecutor) Execute(_ context.Context, _, _ string) (string, error) {
	return e.result, nil
}

func (e *simpleToolExecutor) ResolveToolNames(names []string) tools.ResolvedTools {
	return tools.ResolvedTools{}
}

func TestChat_CancelledSecondRound_CleansUpAllMessages(t *testing.T) {
	firstResp := &provider.Response{
		Content: "",
		ToolCalls: []provider.ToolCall{{
			ID: "tc1", Type: "function",
			Function: provider.FunctionCall{Name: "test_tool", Arguments: "{}"},
		}},
	}

	blockCh := make(chan struct{})
	p := &multiRoundProvider{
		responses: []*provider.Response{firstResp},
		blockCh:   blockCh,
	}

	dir := t.TempDir()
	db, _ := database.Open(filepath.Join(dir, "test.db"))
	t.Cleanup(func() { db.Close() })

	store := session.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte("Be helpful."), 0o644)

	executor := &simpleToolExecutor{result: "tool output"}

	agent := New(Config{
		Provider:       p,
		Sessions:       store,
		ContextBuilder: NewContextBuilder(profilePath),
		ToolExecutor:   executor,
		EventBus:       eventBus,
		Model:          "m",
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := agent.Chat(ctx, ChatRequest{
			SessionKey: "multi-round-cancel",
			Channel:    "web",
			Message:    "Do it",
		})
		errCh <- err
	}()

	// Wait for the first round to complete and the second LLM call to start blocking.
	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-errCh
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}

	// Verify: user message + system "[cancelled]" only.
	// All intermediate messages (assistant with tool calls, tool result) should be deleted.
	msgs, _ := store.GetMessages("multi-round-cancel", 0)
	if len(msgs) != 2 {
		for i, m := range msgs {
			t.Logf("  msg[%d]: role=%s content=%q", i, m.Role, m.Content)
		}
		t.Fatalf("expected 2 messages (user + system), got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("first message should be user, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "system" || msgs[1].Content != "[cancelled]" {
		t.Errorf("second message should be system '[cancelled]', got %q %q", msgs[1].Role, msgs[1].Content)
	}

	close(blockCh)
}
