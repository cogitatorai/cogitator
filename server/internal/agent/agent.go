package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/budget"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/fileproc"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"github.com/cogitatorai/cogitator/server/internal/tools"
)

// ErrCancelled is returned by Chat when the request is cancelled by the caller.
var ErrCancelled = errors.New("agent: cancelled")

const maxToolResultBytes = 32 * 1024 // 32KB per tool result

// truncateToolResult caps a tool result string to max bytes, appending a notice
// so the model knows content was cut and can retry with a more targeted command.
func truncateToolResult(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n\n[output truncated: " + strconv.Itoa(len(s)) + " bytes, showing first " + strconv.Itoa(max) + "]"
}

// ToolExecutor handles the execution of a tool call and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, arguments string) (string, error)
	ResolveToolNames(names []string) tools.ResolvedTools
}

// MemoryRetriever retrieves relevant memories for a given user message.
// It returns a formatted string suitable for injection into the system prompt,
// or an empty string if no relevant memories are found. The history slice
// provides recent conversation context for vector-based retrieval.
type MemoryRetriever interface {
	Retrieve(ctx context.Context, userID, message string, history []provider.Message) (string, error)
}

// UsageRecorder persists token consumption data per chat interaction.
type UsageRecorder interface {
	RecordTokenUsage(tier, model string, tokensIn, tokensOut int, taskRunID *int64, sessionKey string, userID *string) error
}

// MCPServerLister provides a snapshot of configured MCP servers for prompt building.
type MCPServerLister interface {
	Servers() []MCPServerInfo
}

// ConnectorStatusProvider returns connector statuses for a given user.
type ConnectorStatusProvider interface {
	ConnectorStatuses(userID string) []ConnectorStatus
}

// SkillSummary is a lightweight descriptor for an installed skill.
type SkillSummary struct {
	NodeID  string
	Name    string
	Summary string
}

// SkillLister provides a snapshot of installed skills for prompt building.
type SkillLister interface {
	SkillSummaries() []SkillSummary
}

// Agent is the core conversation loop. It builds context, calls the LLM,
// handles iterative tool calls, persists messages, and emits events.
type Agent struct {
	mu             sync.RWMutex
	provider       provider.Provider
	modelProviders map[string]provider.Provider // per-model provider overrides
	sessions       *session.Store
	contextBuilder *ContextBuilder
	toolExecutor   ToolExecutor
	retriever      MemoryRetriever
	tools          []provider.Tool
	eventBus       *bus.Bus
	model          string
	maxToolRounds  int
	usageRecorder  UsageRecorder
	budgetGuard    *budget.Guard
	mcpServers     MCPServerLister
	connectors     ConnectorStatusProvider
	skills         SkillLister
	logger         *slog.Logger
}

type Config struct {
	Provider       provider.Provider
	Sessions       *session.Store
	ContextBuilder *ContextBuilder
	ToolExecutor   ToolExecutor
	Retriever      MemoryRetriever
	Tools          []provider.Tool
	EventBus       *bus.Bus
	Model          string
	UsageRecorder  UsageRecorder
	BudgetGuard    *budget.Guard
	MCPServers     MCPServerLister
	Connectors     ConnectorStatusProvider
	Skills         SkillLister
	MaxToolRounds  int
	Logger         *slog.Logger
}

func New(cfg Config) *Agent {
	maxRounds := cfg.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 25
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{
		provider:       cfg.Provider,
		sessions:       cfg.Sessions,
		contextBuilder: cfg.ContextBuilder,
		toolExecutor:   cfg.ToolExecutor,
		retriever:      cfg.Retriever,
		tools:          cfg.Tools,
		eventBus:       cfg.EventBus,
		model:          cfg.Model,
		usageRecorder:  cfg.UsageRecorder,
		budgetGuard:    cfg.BudgetGuard,
		mcpServers:     cfg.MCPServers,
		connectors:     cfg.Connectors,
		skills:         cfg.Skills,
		maxToolRounds:  maxRounds,
		logger:         logger,
	}
}

type ChatRequest struct {
	SessionKey       string
	Channel          string
	ChatID           string
	UserID           string
	UserName         string
	UserRole         string
	Private          bool
	Message          string
	Attachments      []fileproc.ContentBlock // File content blocks to prepend.
	Summary          string
	Memory           string
	ModelOverride    string // If set, use this model instead of the agent's default.
	ProfileOverrides string // Per-user JSON overrides merged into the system prompt.
}

type ChatResponse struct {
	Content   string
	Usage     provider.Usage
	ToolsUsed *tools.ResolvedTools
}

// SetProvider swaps the default LLM provider at runtime and clears any
// per-model overrides (callers should re-register them after this call).
func (a *Agent) SetProvider(p provider.Provider, model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.provider = p
	a.modelProviders = nil
	if model != "" {
		a.model = model
	}
}

// SetTools replaces the tool list used in subsequent LLM calls.
// Called when MCP servers discover new tools.
func (a *Agent) SetTools(tools []provider.Tool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tools = tools
}

// SetModelProvider registers a provider override for a specific model name.
// When Chat is called with this model (via ModelOverride), the override is
// used instead of the default provider.
func (a *Agent) SetModelProvider(model string, p provider.Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.modelProviders == nil {
		a.modelProviders = make(map[string]provider.Provider)
	}
	a.modelProviders[model] = p
}

// ProviderConfigured reports whether an LLM provider is set.
func (a *Agent) ProviderConfigured() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.provider != nil
}

// RunTask executes a task prompt through the full agent loop (with tools).
// It uses a dedicated session key so task runs don't pollute chat history.
// The model parameter overrides the agent's default model for this call.
func (a *Agent) RunTask(ctx context.Context, sessionKey, prompt, model, userID string) (string, error) {
	// Prefix the task prompt with an execution directive so the LLM performs
	// the action directly instead of trying to schedule or delegate it.
	taskPrompt := "[TASK EXECUTION] You are executing a scheduled task. " +
		"Perform the following action directly and return the result. " +
		"Do NOT use create_task, list_tasks, delete_task, or run_task.\n\n" +
		prompt
	resp, err := a.Chat(ctx, ChatRequest{
		SessionKey:    sessionKey,
		Channel:       "task",
		Message:       taskPrompt,
		ModelOverride: model,
		UserID:        userID,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (a *Agent) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	a.mu.RLock()
	p := a.provider
	model := a.model
	if req.ModelOverride != "" {
		model = req.ModelOverride
		if mp, ok := a.modelProviders[model]; ok {
			p = mp
		}
	}
	a.mu.RUnlock()

	if p == nil {
		return nil, fmt.Errorf("no LLM provider configured")
	}

	// Enforce budget and rate limits before doing any work.
	if a.budgetGuard != nil {
		tier := "standard"
		if model != a.model {
			tier = "cheap"
		}
		if err := a.budgetGuard.Allow(tier, req.UserID); err != nil {
			return nil, err
		}
	}

	a.logger.Info("chat request",
		"session_key", req.SessionKey,
		"channel", req.Channel,
		"model", model,
		"message_len", len(req.Message),
	)

	if a.eventBus != nil {
		a.eventBus.Publish(bus.Event{
			Type: bus.MessageReceived,
			Payload: map[string]any{
				"session_key": req.SessionKey,
				"channel":     req.Channel,
				"message":     req.Message,
			},
		})
	}

	// Ensure session exists
	if a.sessions != nil {
		_, err := a.sessions.GetOrCreate(req.SessionKey, req.Channel, req.ChatID, req.UserID, req.Private)
		if err != nil {
			return nil, fmt.Errorf("session get or create: %w", err)
		}
	}

	// Build content for storage.
	var contentForStorage string
	if len(req.Attachments) > 0 {
		blocks := make([]fileproc.ContentBlock, len(req.Attachments))
		copy(blocks, req.Attachments)
		if req.Message != "" {
			blocks = append(blocks, fileproc.ContentBlock{Type: "text", Text: req.Message})
		}
		raw, _ := json.Marshal(blocks)
		contentForStorage = string(raw)
	} else {
		contentForStorage = req.Message
	}

	// Persist the user message
	if a.sessions != nil {
		a.sessions.AddMessage(req.SessionKey, session.Message{
			Role:    "user",
			Content: contentForStorage,
		})
	}

	// Load conversation history
	var history []session.Message
	if a.sessions != nil {
		var err error
		history, err = a.sessions.GetMessages(req.SessionKey, 50)
		if err != nil {
			a.logger.Warn("failed to load history", "error", err)
		}
		// Remove the last message from history since we already include it
		// as the current message in the context builder.
		if len(history) > 0 {
			history = history[:len(history)-1]
		}
	}

	// Retrieve relevant memories if none were provided by the caller.
	if req.Memory == "" && a.retriever != nil {
		var provHistory []provider.Message
		for _, m := range history {
			provHistory = append(provHistory, provider.Message{Role: m.Role, Content: m.Content})
		}
		if memCtx, err := a.retriever.Retrieve(ctx, req.UserID, req.Message, provHistory); err == nil && memCtx != "" {
			req.Memory = memCtx
		} else if err != nil {
			a.logger.Warn("memory retrieval failed", "error", err)
		}
	}

	firstMessage := len(history) == 0

	// Generate a session title immediately from the user's prompt so the
	// sidebar updates before the agent produces a response.
	if firstMessage && p != nil && a.sessions != nil {
		go a.generateTitle(p, model, req.SessionKey, req.Message)
	}

	// Build prompt and messages
	var mcpInfo []MCPServerInfo
	if a.mcpServers != nil {
		mcpInfo = a.mcpServers.Servers()
	}
	var connectorInfo []ConnectorStatus
	if a.connectors != nil {
		connectorInfo = a.connectors.ConnectorStatuses(req.UserID)
	}
	var skillInfo []SkillSummary
	if a.skills != nil {
		skillInfo = a.skills.SkillSummaries()
	}
	a.logger.Info("context: skills for prompt", "count", len(skillInfo))
	systemPrompt := a.contextBuilder.BuildSystemPrompt(req.Summary, req.Memory, mcpInfo, connectorInfo, skillInfo, req.ProfileOverrides, UserContext{Name: req.UserName})
	messages := a.contextBuilder.BuildMessages(systemPrompt, history, req.Message, req.Attachments)

	// Inject chat scope so downstream tools (e.g. save_memory) can
	// determine memory ownership based on user and privacy mode.
	ctx = tools.WithChatScope(ctx, tools.ChatScope{
		UserID:  req.UserID,
		Role:    req.UserRole,
		Private: req.Private,
	})

	// Agentic loop: call LLM, handle tool calls, repeat
	var totalUsage provider.Usage
	var rawToolNames []string

	var pendingMsgIDs []int64
	trackMsg := func(id int64, err error) {
		if err == nil && id > 0 {
			pendingMsgIDs = append(pendingMsgIDs, id)
		}
	}

	cancelCleanup := func() {
		if a.sessions != nil {
			for _, id := range pendingMsgIDs {
				a.sessions.DeleteMessage(id)
			}
			a.sessions.AddMessage(req.SessionKey, session.Message{
				Role:    "system",
				Content: "[cancelled]",
			})
		}
		if a.eventBus != nil {
			a.eventBus.Publish(bus.Event{
				Type: bus.AgentCancelled,
				Payload: map[string]any{
					"session_key": req.SessionKey,
				},
			})
		}
	}

	for round := 0; round <= a.maxToolRounds; round++ {
		if ctx.Err() != nil {
			cancelCleanup()
			return nil, ErrCancelled
		}

		if a.eventBus != nil {
			a.eventBus.Publish(bus.Event{
				Type: bus.AgentThinking,
				Payload: map[string]any{
					"session_key": req.SessionKey,
					"round":       round,
				},
			})
		}

		resp, err := a.callProvider(ctx, p, messages, a.tools, model)
		if err != nil {
			if ctx.Err() != nil {
				cancelCleanup()
				return nil, ErrCancelled
			}
			return nil, fmt.Errorf("provider chat: %w", err)
		}

		totalUsage.InputTokens += resp.Usage.InputTokens
		totalUsage.OutputTokens += resp.Usage.OutputTokens

		// No tool calls: we have the final response
		if len(resp.ToolCalls) == 0 {
			// Resolve tool names into display-ready groups.
			var resolved *tools.ResolvedTools
			if len(rawToolNames) > 0 && a.toolExecutor != nil {
				r := a.toolExecutor.ResolveToolNames(rawToolNames)
				resolved = &r
			}

			// Serialize resolved tools for persistence.
			var toolsUsedJSON string
			if resolved != nil {
				if data, err := json.Marshal(resolved); err == nil {
					toolsUsedJSON = string(data)
				}
			}

			// Persist assistant message
			if a.sessions != nil {
				a.sessions.AddMessage(req.SessionKey, session.Message{
					Role:      "assistant",
					Content:   resp.Content,
					ToolsUsed: toolsUsedJSON,
				})
			}

			if a.eventBus != nil {
				a.eventBus.Publish(bus.Event{
					Type: bus.MessageResponded,
					Payload: map[string]any{
						"session_key":   req.SessionKey,
						"channel":       req.Channel,
						"content":       resp.Content,
						"input_tokens":  totalUsage.InputTokens,
						"output_tokens": totalUsage.OutputTokens,
					},
				})
			}

	
			// Record token usage for analytics.
			if a.usageRecorder != nil {
				tier := "standard"
				if model != a.model {
					tier = "cheap"
				}
				var uid *string
				if req.UserID != "" {
					uid = &req.UserID
				}
				_ = a.usageRecorder.RecordTokenUsage(tier, model, totalUsage.InputTokens, totalUsage.OutputTokens, nil, req.SessionKey, uid)
			}

			return &ChatResponse{
				Content:   resp.Content,
				Usage:     totalUsage,
				ToolsUsed: resolved,
			}, nil
		}

		// Append the assistant message with tool calls to the conversation
		assistantMsg := provider.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// Persist assistant message with tool calls so that subsequent tool-result
		// messages in history always have the required preceding tool_calls entry.
		if a.sessions != nil {
			toolCallsJSON, _ := json.Marshal(resp.ToolCalls)
			trackMsg(a.sessions.AddMessage(req.SessionKey, session.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: string(toolCallsJSON),
			}))
		}

		// Execute each tool call and collect names for resolution.
		for _, tc := range resp.ToolCalls {
			if ctx.Err() != nil {
				cancelCleanup()
				return nil, ErrCancelled
			}
			rawToolNames = append(rawToolNames, tc.Function.Name)
			if a.eventBus != nil {
				a.eventBus.Publish(bus.Event{
					Type: bus.AgentToolCalling,
					Payload: map[string]any{
						"session_key": req.SessionKey,
						"tool":        tc.Function.Name,
					},
				})
			}

			result := "Tool execution not available"
			if a.toolExecutor != nil {
				a.logger.Info("tool call",
					"session_key", req.SessionKey,
					"tool", tc.Function.Name,
					"arguments", tc.Function.Arguments,
					"round", round,
				)
				toolStart := time.Now()
				var execErr error
				result, execErr = a.toolExecutor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
				toolElapsed := time.Since(toolStart)
				if execErr != nil {
					a.logger.Warn("tool call failed",
						"session_key", req.SessionKey,
						"tool", tc.Function.Name,
						"error", execErr,
						"elapsed", toolElapsed.String(),
					)
					if result == "" {
						result = fmt.Sprintf("Error: %v", execErr)
					}
				} else {
					a.logger.Info("tool call completed",
						"session_key", req.SessionKey,
						"tool", tc.Function.Name,
						"result_len", len(result),
						"elapsed", toolElapsed.String(),
					)
				}
				if collector := task.ToolCallCollectorFromContext(ctx); collector != nil {
					collector.Record(tc.Function.Name, tc.Function.Arguments, result, toolElapsed, round, execErr)
				}
			}

			result = truncateToolResult(result, maxToolResultBytes)

			toolMsg := provider.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolMsg)

			// Persist tool result
			if a.sessions != nil {
				trackMsg(a.sessions.AddMessage(req.SessionKey, session.Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
				}))
			}
		}
	}

	return nil, fmt.Errorf("exceeded maximum tool call rounds (%d)", a.maxToolRounds)
}

// providerResult bundles the return values of a provider Chat call so they
// can be sent through a channel.
type providerResult struct {
	resp *provider.Response
	err  error
}

// callProvider wraps an LLM call with cancellation-aware behavior. Providers
// that advertise StreamCancel receive the caller's context directly so they
// can abort mid-stream. For all others the HTTP call is detached into a
// background goroutine; if the caller's context is cancelled before the
// provider returns, the result is silently discarded.
func (a *Agent) callProvider(ctx context.Context, p provider.Provider, messages []provider.Message, tools []provider.Tool, model string) (*provider.Response, error) {
	canStreamCancel := false
	if cp, ok := p.(provider.CapabilityProvider); ok {
		canStreamCancel = cp.Capabilities().StreamCancel
	}

	if canStreamCancel {
		return p.Chat(ctx, messages, tools, model, nil)
	}

	bgCtx, bgCancel := context.WithTimeout(context.Background(), 120*time.Second)
	ch := make(chan providerResult, 1)
	go func() {
		defer bgCancel()
		resp, err := p.Chat(bgCtx, messages, tools, model, nil)
		ch <- providerResult{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		bgCancel()
		return r.resp, r.err
	}
}

// generateTitle asks the LLM for a short session title based on the user's
// first message, then persists it and emits a bus event. Runs in a background
// goroutine so the sidebar updates before the agent produces a response.
// A brief pause lets the main chat call reach the provider first, avoiding
// contention on rate-limited or sequential providers.
func (a *Agent) generateTitle(p provider.Provider, model, sessionKey, userMsg string) {
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	messages := []provider.Message{
		{Role: "system", Content: "Generate a short title (max 6 words) for a conversation that starts with the following message. Respond with ONLY the title, no quotes or punctuation."},
		{Role: "user", Content: userMsg},
	}

	resp, err := p.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		a.logger.Warn("failed to generate session title", "error", err)
		return
	}

	title := strings.TrimSpace(resp.Content)
	if title == "" {
		return
	}
	// Cap at 80 characters to keep sidebar tidy.
	if len(title) > 80 {
		title = title[:80]
	}

	if err := a.sessions.SetSummary(sessionKey, title); err != nil {
		a.logger.Warn("failed to set session summary", "error", err)
		return
	}

	if a.eventBus != nil {
		a.eventBus.Publish(bus.Event{
			Type: bus.SessionTitleSet,
			Payload: map[string]any{
				"session_key": sessionKey,
				"title":       title,
			},
		})
	}
}
