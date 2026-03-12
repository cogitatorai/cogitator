package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

const initialProfile = `## Communication
- Default to concise, structured output. [evidence: node_abc]

## Task Execution
- Always verify API responses before processing. [evidence: node_def]
`

const revisedProfile = `## Communication
- Default to concise, structured output. [evidence: node_abc]
- Use bullet points for lists. [evidence: node_xyz]

## Task Execution
- Always verify API responses before processing. [evidence: node_def]
`

func setupProfiler(t *testing.T, mockResp string) (*Profiler, *bus.Bus, string) {
	t.Helper()
	db := testDB(t)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")

	if err := os.WriteFile(profilePath, []byte(initialProfile), 0o644); err != nil {
		t.Fatalf("write initial profile: %v", err)
	}

	mock := provider.NewMock(provider.Response{Content: mockResp})

	p := NewProfiler(ProfilerConfig{
		Memory:      memStore,
		Provider:    mock,
		EventBus:    eventBus,
		Model:       "test-model",
		ProfilePath: profilePath,
		Logger:      nil,
	})

	return p, eventBus, profilePath
}

func addTestNodes(t *testing.T, memStore *memory.Store) {
	t.Helper()
	nodes := []memory.Node{
		{
			Type:      memory.NodeEpisode,
			Title:     "User corrected verbosity",
			Summary:   "User asked for shorter responses after a long explanation",
			Confidence: 0.9,
		},
		{
			Type:      memory.NodePreference,
			Title:     "Prefers bullet points",
			Summary:   "User explicitly prefers bullet-point lists over prose",
			Confidence: 0.85,
		},
	}
	for _, n := range nodes {
		if _, err := memStore.CreateNode(&n); err != nil {
			t.Fatalf("create node %q: %v", n.Title, err)
		}
	}
}

func TestProfilerRevisesProfile(t *testing.T) {
	db := testDB(t)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(initialProfile), 0o644); err != nil {
		t.Fatalf("write initial profile: %v", err)
	}

	addTestNodes(t, memStore)

	mock := provider.NewMock(provider.Response{Content: revisedProfile})

	p := NewProfiler(ProfilerConfig{
		Memory:      memStore,
		Provider:    mock,
		EventBus:    eventBus,
		Model:       "test-model",
		ProfilePath: profilePath,
	})

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	eventBus.Publish(bus.Event{Type: bus.ProfileRevisionDue})

	// Wait for the profile file to be updated.
	waitFor(t, 3*time.Second, func() bool {
		content, err := os.ReadFile(profilePath)
		return err == nil && string(content) != initialProfile
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(content) == initialProfile {
		t.Error("expected profile to be updated, but it was unchanged")
	}

	// Verify the backup exists.
	backupPath := profilePath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("expected backup file to exist, but it does not")
	}

	// Verify the LLM was called once.
	if n := mock.CallCount(); n != 1 {
		t.Errorf("expected 1 LLM call, got %d", n)
	}

	// Verify the prompt included evidence node titles.
	calls := mock.GetCalls()
	if len(calls[0]) == 0 {
		t.Fatal("LLM call had no messages")
	}
	promptContent := calls[0][0].ContentText()
	if !containsString(promptContent, "User corrected verbosity") {
		t.Error("prompt did not include episode node title")
	}
	if !containsString(promptContent, "Prefers bullet points") {
		t.Error("prompt did not include preference node title")
	}
}

func TestProfilerNoChanges(t *testing.T) {
	// The LLM returns the same profile content unchanged.
	p, eventBus, profilePath := setupProfiler(t, initialProfile)

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	eventBus.Publish(bus.Event{Type: bus.ProfileRevisionDue})

	// Wait for the LLM call to complete (the file write still happens even if
	// content is the same, so we check for the backup which only appears after
	// the write cycle).
	backupPath := profilePath + ".bak"
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(backupPath)
		return err == nil
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	// The profile content should match TrimSpace(response) + "\n", which is how
	// the profiler normalises the LLM output before writing.
	wantContent := strings.TrimSpace(initialProfile) + "\n"
	if string(content) != wantContent {
		t.Errorf("expected profile content to be the initial profile (trimmed + newline), got:\n%q", string(content))
	}
}

func TestProfilerCreatesBackup(t *testing.T) {
	p, eventBus, profilePath := setupProfiler(t, revisedProfile)

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	backupPath := profilePath + ".bak"

	// Confirm no backup exists before the event.
	if _, err := os.Stat(backupPath); err == nil {
		t.Fatal("backup should not exist before revision")
	}

	eventBus.Publish(bus.Event{Type: bus.ProfileRevisionDue})

	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(backupPath)
		return err == nil
	})

	// Verify backup content matches the original profile.
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContent) != initialProfile {
		t.Errorf("backup content mismatch.\ngot:  %q\nwant: %q", string(backupContent), initialProfile)
	}

	// Verify the live profile now holds the revised content (normalised).
	liveContent, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read live profile: %v", err)
	}
	wantLive := strings.TrimSpace(revisedProfile) + "\n"
	if string(liveContent) != wantLive {
		t.Errorf("live profile content mismatch.\ngot:  %q\nwant: %q", string(liveContent), wantLive)
	}
}

func TestProfilerRegenOnMemoryCount(t *testing.T) {
	// Use a threshold of 2: after 2 EnrichmentQueued events the profile must be regenerated.
	db := testDB(t)
	memStore := memory.NewStore(db)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(initialProfile), 0o644); err != nil {
		t.Fatalf("write initial profile: %v", err)
	}

	mock := provider.NewMock(provider.Response{Content: revisedProfile})

	p := NewProfiler(ProfilerConfig{
		Memory:         memStore,
		Provider:       mock,
		EventBus:       eventBus,
		Model:          "test-model",
		ProfilePath:    profilePath,
		RegenThreshold: 2,
	})

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	// Fire two EnrichmentQueued events to reach the threshold.
	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})
	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})

	// Wait for the profile to be rewritten.
	waitFor(t, 3*time.Second, func() bool {
		content, err := os.ReadFile(profilePath)
		return err == nil && string(content) != initialProfile
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(content) == initialProfile {
		t.Error("expected profile to be updated after N EnrichmentQueued events, but it was unchanged")
	}

	// Verify the LLM was called exactly once.
	if n := mock.CallCount(); n != 1 {
		t.Errorf("expected 1 LLM call after threshold, got %d", n)
	}
}

// containsString is a test helper that checks whether s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsRuneSearch(s, substr))
}

func containsRuneSearch(s, substr string) bool {
	for i := range s {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
