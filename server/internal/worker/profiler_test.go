package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
)

const initialProfile = `## Communication
- Default to concise, structured output. [evidence: node_abc]

## Task Execution
- Always verify API responses before processing. [evidence: node_def]
`

func setupProfiler(t *testing.T) (*Profiler, *bus.Bus, string) {
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

	p := NewProfiler(ProfilerConfig{
		Memory:      memStore,
		EventBus:    eventBus,
		ProfilePath: profilePath,
		Logger:      nil,
	})

	return p, eventBus, profilePath
}

func addTestNodes(t *testing.T, memStore *memory.Store) {
	t.Helper()
	nodes := []memory.Node{
		{
			Type:       memory.NodeEpisode,
			Title:      "User corrected verbosity",
			Summary:    "User asked for shorter responses after a long explanation",
			Confidence: 0.9,
		},
		{
			Type:       memory.NodePreference,
			Title:      "Prefers bullet points",
			Summary:    "User explicitly prefers bullet-point lists over prose",
			Tags:       []string{"communication"},
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

	p := NewProfiler(ProfilerConfig{
		Memory:      memStore,
		EventBus:    eventBus,
		ProfilePath: profilePath,
	})

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	eventBus.Publish(bus.Event{Type: bus.ProfileRevisionDue})

	// Wait for the backup to appear, which signals the write cycle completed.
	backupPath := profilePath + ".bak"
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(backupPath)
		return err == nil
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	if !strings.Contains(string(content), "Prefers bullet points") {
		t.Error("revised profile should contain the preference node")
	}
	if !strings.Contains(string(content), "User corrected verbosity") {
		t.Error("revised profile should contain the episode node")
	}

	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContent) != initialProfile {
		t.Errorf("backup content mismatch.\ngot:  %q\nwant: %q", string(backupContent), initialProfile)
	}
}

func TestProfilerNoChanges(t *testing.T) {
	// With an empty store the generated profile is the template skeleton.
	p, eventBus, profilePath := setupProfiler(t)

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	eventBus.Publish(bus.Event{Type: bus.ProfileRevisionDue})

	// Wait for the backup to appear.
	backupPath := profilePath + ".bak"
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(backupPath)
		return err == nil
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	// The profile must be a valid structured document.
	if !strings.Contains(string(content), "## Identity") {
		t.Error("generated profile should contain Identity section")
	}
	if !strings.Contains(string(content), "## Preferences") {
		t.Error("generated profile should contain Preferences section")
	}
}

func TestProfilerCreatesBackup(t *testing.T) {
	p, eventBus, profilePath := setupProfiler(t)

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

	// Verify the live profile is structured output (not the initial profile).
	liveContent, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read live profile: %v", err)
	}
	if !strings.Contains(string(liveContent), "## Identity") {
		t.Error("live profile should contain structured sections")
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

	p := NewProfiler(ProfilerConfig{
		Memory:         memStore,
		EventBus:       eventBus,
		ProfilePath:    profilePath,
		RegenThreshold: 2,
	})

	ctx := t.Context()
	p.Start(ctx)
	defer p.Stop()

	// Fire two EnrichmentQueued events to reach the threshold.
	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})
	eventBus.Publish(bus.Event{Type: bus.EnrichmentQueued})

	// Wait for the backup to appear.
	backupPath := profilePath + ".bak"
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(backupPath)
		return err == nil
	})

	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if !strings.Contains(string(content), "## Identity") {
		t.Error("expected structured profile after N EnrichmentQueued events")
	}
}

func TestBuildStructuredProfile(t *testing.T) {
	facts := []memory.Node{
		{Title: "Name is Andrei", Summary: "the user's name is Andrei", Tags: []string{"name", "identity"}},
		{Title: "Lives in Meudon", Summary: "the user lives in Meudon", Tags: []string{"location", "identity"}},
	}
	prefs := []memory.Node{
		{Title: "Likes hiking", Summary: "the user enjoys hiking", Tags: []string{"outdoor"}},
		{Title: "Dark roast coffee", Summary: "the user prefers dark roast", Tags: []string{"food"}},
	}
	patterns := []memory.Node{
		{Title: "Pattern: outdoor, activity", Summary: "Recurring theme across 3 memories"},
	}
	episodes := []memory.Node{
		{Title: "Correction: not Guillaume", Summary: "the user corrected person attribution"},
	}

	profile := buildStructuredProfile(facts, prefs, patterns, episodes)

	if !strings.Contains(profile, "## Identity") {
		t.Error("profile should have Identity section")
	}
	if !strings.Contains(profile, "Andrei") {
		t.Error("profile should mention Andrei")
	}
	if !strings.Contains(profile, "## Preferences") {
		t.Error("profile should have Preferences section")
	}
	if !strings.Contains(profile, "hiking") {
		t.Error("profile should mention hiking")
	}
	if !strings.Contains(profile, "## Behavioral Patterns") {
		t.Error("profile should have Behavioral Patterns section")
	}
	if !strings.Contains(profile, "outdoor") {
		t.Error("profile should mention outdoor pattern")
	}
	if !strings.Contains(profile, "## Communication Notes") {
		t.Error("profile should have Communication Notes section")
	}
	if !strings.Contains(profile, "Guillaume") {
		t.Error("profile should mention the correction episode")
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
