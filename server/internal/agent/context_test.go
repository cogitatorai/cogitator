package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/session"
)

func TestBuildSystemPromptBasic(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte("- Be concise. [evidence: node_abc]"), 0o644)

	cb := NewContextBuilder(profilePath)
	prompt := cb.BuildSystemPrompt("", "", nil, nil, nil, "", UserContext{})

	if !strings.Contains(prompt, "Cogitator") {
		t.Error("expected prompt to contain 'Cogitator'")
	}
	if !strings.Contains(prompt, "Be concise") {
		t.Error("expected prompt to contain profile content")
	}
}

func TestBuildSystemPromptWithSummary(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	cb := NewContextBuilder(profilePath)
	prompt := cb.BuildSystemPrompt("User discussed Go programming.", "", nil, nil, nil, "", UserContext{})

	if !strings.Contains(prompt, "User discussed Go programming.") {
		t.Error("expected prompt to contain summary")
	}
}

func TestBuildSystemPromptWithMemory(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	cb := NewContextBuilder(profilePath)
	prompt := cb.BuildSystemPrompt("", "User prefers TypeScript for frontend work.", nil, nil, nil, "", UserContext{})

	if !strings.Contains(prompt, "Retrieved Memories") {
		t.Error("expected prompt to contain memory section")
	}
	if !strings.Contains(prompt, "TypeScript") {
		t.Error("expected prompt to contain memory content")
	}
}

func TestBuildSystemPromptMissingProfile(t *testing.T) {
	cb := NewContextBuilder("/nonexistent/profile.md")
	prompt := cb.BuildSystemPrompt("", "", nil, nil, nil, "", UserContext{})

	if !strings.Contains(prompt, "Cogitator") {
		t.Error("expected prompt to still contain identity")
	}
	if strings.Contains(prompt, "Behavioral Profile") {
		t.Error("should not contain profile section when file missing")
	}
}

func TestBuildMessages(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	history := []session.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	messages := cb.BuildMessages("system prompt", history, "How are you?", nil)

	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	if messages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %q", messages[0].Role)
	}
	if messages[0].Content != "system prompt" {
		t.Errorf("expected system prompt content")
	}
	if messages[1].Role != "user" || messages[1].Content != "Hello" {
		t.Error("expected history messages preserved")
	}
	if messages[3].Role != "user" || messages[3].Content != "How are you?" {
		t.Error("expected current message appended")
	}
}

func TestBuildMessagesEmptyCurrentMessage(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	messages := cb.BuildMessages("system", nil, "   ", nil)

	if len(messages) != 1 {
		t.Errorf("expected 1 message (system only), got %d", len(messages))
	}
}

func TestBuildMessagesRestoresToolCalls(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	history := []session.Message{
		{Role: "user", Content: "search for weather skills"},
		{
			Role:      "assistant",
			Content:   "",
			ToolCalls: `[{"id":"call_1","type":"function","function":{"name":"search_skills","arguments":"{\"query\":\"weather\"}"}}]`,
		},
		{
			Role:       "tool",
			Content:    `[{"slug":"weather","display_name":"Weather"}]`,
			ToolCallID: "call_1",
		},
		{Role: "assistant", Content: "I found a weather skill."},
	}

	messages := cb.BuildMessages("system", history, "Install it", nil)

	// system + 4 history + current user = 6
	if len(messages) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(messages))
	}

	// The assistant message at index 2 must have ToolCalls restored.
	assistantWithTools := messages[2]
	if len(assistantWithTools.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on assistant message, got %d", len(assistantWithTools.ToolCalls))
	}
	if assistantWithTools.ToolCalls[0].ID != "call_1" {
		t.Errorf("tool call ID = %q, want %q", assistantWithTools.ToolCalls[0].ID, "call_1")
	}
	if assistantWithTools.ToolCalls[0].Function.Name != "search_skills" {
		t.Errorf("tool call function name = %q, want %q", assistantWithTools.ToolCalls[0].Function.Name, "search_skills")
	}

	// The tool message at index 3 must have ToolCallID set.
	toolMsg := messages[3]
	if toolMsg.ToolCallID != "call_1" {
		t.Errorf("tool message ToolCallID = %q, want %q", toolMsg.ToolCallID, "call_1")
	}
}

func TestBuildMessagesParallelToolCalls(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	// Simulate an assistant message with 4 parallel tool calls followed
	// by 4 tool-result messages (the pattern that triggers the bug when
	// the sequence-tracking flag resets after the first tool message).
	history := []session.Message{
		{Role: "user", Content: "build a task"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: `[` +
				`{"id":"call_A","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}},` +
				`{"id":"call_B","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"b.txt\"}"}},` +
				`{"id":"call_C","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"c.txt\"}"}},` +
				`{"id":"call_D","type":"function","function":{"name":"shell","arguments":"{\"command\":\"echo ok\"}"}}` +
				`]`,
		},
		{Role: "tool", Content: "contents of a", ToolCallID: "call_A"},
		{Role: "tool", Content: "contents of b", ToolCallID: "call_B"},
		{Role: "tool", Content: "contents of c", ToolCallID: "call_C"},
		{Role: "tool", Content: "ok", ToolCallID: "call_D"},
		{Role: "assistant", Content: "Done, I created the task."},
	}

	messages := cb.BuildMessages("system", history, "thanks", nil)

	// system + 7 history + current user = 9
	if len(messages) != 9 {
		t.Fatalf("expected 9 messages, got %d", len(messages))
	}

	// Verify all 4 tool results are present.
	toolIDs := map[string]bool{}
	for _, m := range messages {
		if m.Role == "tool" {
			toolIDs[m.ToolCallID] = true
		}
	}
	for _, id := range []string{"call_A", "call_B", "call_C", "call_D"} {
		if !toolIDs[id] {
			t.Errorf("tool result for %s was dropped from context", id)
		}
	}
}

func TestBuildMessagesDropsOrphanedToolResults(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	// Tool-result messages without a preceding assistant tool_calls
	// message should be silently dropped.
	history := []session.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: "orphaned result", ToolCallID: "call_X"},
		{Role: "assistant", Content: "Hi there!"},
	}

	messages := cb.BuildMessages("system", history, "next", nil)

	// system + user + assistant (orphaned tool dropped) + current user = 4
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
	for _, m := range messages {
		if m.Role == "tool" {
			t.Error("orphaned tool message should have been dropped")
		}
	}
}

func TestBuildMessagesToolResultWithJSONArray(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	// Tool results that happen to be JSON arrays should NOT be parsed as
	// multimodal content blocks. This was causing OpenAI API errors like
	// "Missing required parameter: messages[N].content[0].type".
	history := []session.Message{
		{Role: "user", Content: "search for weather skills"},
		{
			Role:      "assistant",
			Content:   "",
			ToolCalls: `[{"id":"call_1","type":"function","function":{"name":"search_skills","arguments":"{\"query\":\"weather\"}"}}]`,
		},
		{
			Role:       "tool",
			Content:    `[{"slug":"weather-forecast","display_name":"Weather Forecast"}]`,
			ToolCallID: "call_1",
		},
		{Role: "assistant", Content: "I found a weather skill."},
	}

	messages := cb.BuildMessages("system", history, "Install it", nil)

	// The tool message content must remain a plain string, not parsed as blocks.
	toolMsg := messages[3]
	if toolMsg.Role != "tool" {
		t.Fatalf("expected message[3] to be tool, got %q", toolMsg.Role)
	}
	if _, ok := toolMsg.Content.(string); !ok {
		t.Errorf("tool message content should be string, got %T", toolMsg.Content)
	}
}

func TestBuildMessagesBlocksWithoutType(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")

	// A user message with JSON array content where blocks lack "type"
	// should fall back to plain string to avoid API errors.
	history := []session.Message{
		{Role: "user", Content: `[{"text":"hello"}]`},
		{Role: "assistant", Content: "Hi!"},
	}

	messages := cb.BuildMessages("system", history, "next", nil)

	userMsg := messages[1]
	if _, ok := userMsg.Content.(string); !ok {
		t.Errorf("user message with typeless blocks should remain string, got %T", userMsg.Content)
	}
}

func TestBuildSystemPromptWithUserContext(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")
	uc := UserContext{Name: "Alice"}
	prompt := cb.BuildSystemPrompt("", "", nil, nil, nil, "", uc)

	if !strings.Contains(prompt, "Alice") {
		t.Error("expected prompt to contain user name")
	}
	if !strings.Contains(prompt, "Current User") {
		t.Error("expected prompt to contain Current User section")
	}
}

func TestBuildSystemPromptEmptyUserContext(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")
	prompt := cb.BuildSystemPrompt("", "", nil, nil, nil, "", UserContext{})

	if strings.Contains(prompt, "Current User") {
		t.Error("should not contain Current User section when name is empty")
	}
}

func TestBuildSkillsSection_Empty(t *testing.T) {
	cb := NewContextBuilder("")
	got := cb.buildSkillsSection(nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildSkillsSection_WithSkills(t *testing.T) {
	cb := NewContextBuilder("")
	skills := []SkillSummary{
		{NodeID: "node_1", Name: "CV Coach", Summary: "Analyze and improve resumes."},
		{NodeID: "node_2", Name: "Weather", Summary: ""},
	}
	got := cb.buildSkillsSection(skills)

	if !strings.Contains(got, "## Installed Skills") {
		t.Error("missing header")
	}
	if !strings.Contains(got, "**CV Coach** (node_id: node_1)") {
		t.Error("missing skill name with node_id")
	}
	if !strings.Contains(got, "Analyze and improve resumes.") {
		t.Error("missing skill summary")
	}
	if !strings.Contains(got, "**Weather** (node_id: node_2)") {
		t.Error("missing second skill with node_id")
	}
	if !strings.Contains(got, "MUST call read_skill") {
		t.Error("missing instruction to use read_skill")
	}
}

func TestBuildSystemPromptIncludesSkills(t *testing.T) {
	cb := NewContextBuilder("/nonexistent")
	skills := []SkillSummary{
		{NodeID: "node_1", Name: "CV Coach", Summary: "Analyze and improve resumes."},
	}
	prompt := cb.BuildSystemPrompt("", "", nil, nil, skills, "", UserContext{})

	if !strings.Contains(prompt, "Installed Skills") {
		t.Error("expected prompt to contain skills section")
	}
	if !strings.Contains(prompt, "CV Coach") {
		t.Error("expected prompt to contain skill name")
	}
}

func TestBuildMCPSection_NoServers(t *testing.T) {
	cb := NewContextBuilder("")
	got := cb.buildMCPSection(nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBuildMCPSection_WithServers(t *testing.T) {
	cb := NewContextBuilder("")
	servers := []MCPServerInfo{
		{Name: "github", Status: "running", ToolCount: 5},
		{Name: "travel", Status: "stopped", ToolCount: 0, Instructions: "Travel directions and transit routes"},
	}
	got := cb.buildMCPSection(servers)

	checks := []string{
		"## External Tools (MCP Servers)",
		"mcp__",
		"list_installed_skills",
		"read_skill",
		"server is not running",
		"- github: running (5 tools)",
		"- travel: stopped (0 tools) - Travel directions and transit routes",
		"start_mcp_server",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
