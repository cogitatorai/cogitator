package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
)

// ProfilerConfig holds the dependencies and configuration for the Profiler worker.
type ProfilerConfig struct {
	Memory         *memory.Store
	EventBus       *bus.Bus
	ProfilePath    string
	Logger         *slog.Logger
	RegenThreshold int // number of new memories before profile regeneration (default 5)
}

// Profiler listens for ProfileRevisionDue events and rebuilds the behavioral
// profile on disk from the memory graph. It also counts EnrichmentQueued events
// and triggers revision when the count reaches regenThreshold.
type Profiler struct {
	memory         *memory.Store
	eventBus       *bus.Bus
	profilePath    string
	logger         *slog.Logger
	cancel         context.CancelFunc
	lastRevision   time.Time
	regenThreshold int
	nodeCounter    int32 // accessed atomically
}

// NewProfiler constructs a Profiler from the given config.
func NewProfiler(cfg ProfilerConfig) *Profiler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	threshold := cfg.RegenThreshold
	if threshold <= 0 {
		threshold = 5
	}
	return &Profiler{
		memory:         cfg.Memory,
		eventBus:       cfg.EventBus,
		profilePath:    cfg.ProfilePath,
		logger:         cfg.Logger,
		regenThreshold: threshold,
	}
}

// Start subscribes to ProfileRevisionDue and EnrichmentQueued events and
// processes them in a goroutine.
func (p *Profiler) Start(ctx context.Context) {
	ctx, p.cancel = context.WithCancel(ctx)
	revisionCh := p.eventBus.Subscribe(bus.ProfileRevisionDue)
	enrichCh := p.eventBus.Subscribe(bus.EnrichmentQueued)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-revisionCh:
				if !ok {
					return
				}
				if err := p.revise(ctx, evt); err != nil {
					p.logger.Error("profile revision failed", "err", err)
				}
			case _, ok := <-enrichCh:
				if !ok {
					return
				}
				count := atomic.AddInt32(&p.nodeCounter, 1)
				if int(count) >= p.regenThreshold {
					atomic.StoreInt32(&p.nodeCounter, 0)
					p.logger.Info("profile regeneration triggered by memory count", "count", count)
					if err := p.revise(ctx, bus.Event{}); err != nil {
						p.logger.Error("profile revision failed", "err", err)
					}
				}
			}
		}
	}()

	p.logger.Info("profiler started", "profile", p.profilePath, "regen_threshold", p.regenThreshold)
}

// Stop cancels the worker's context, stopping the event loop.
func (p *Profiler) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// revise queries the memory graph for all node types, builds a structured
// profile from the results, and writes it to disk (backing up the previous
// version first). No LLM call is made.
func (p *Profiler) revise(_ context.Context, _ bus.Event) error {
	// Load the current profile for backup purposes. Missing file is fine.
	currentProfile, err := os.ReadFile(p.profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read profile: %w", err)
	}

	// Query the graph for all four node types.
	facts, err := p.memory.ListNodes("", memory.NodeFact, 100, 0)
	if err != nil {
		return fmt.Errorf("list facts: %w", err)
	}
	prefs, err := p.memory.ListNodes("", memory.NodePreference, 100, 0)
	if err != nil {
		return fmt.Errorf("list preferences: %w", err)
	}
	patterns, err := p.memory.ListNodes("", memory.NodePattern, 50, 0)
	if err != nil {
		return fmt.Errorf("list patterns: %w", err)
	}
	episodes, err := p.memory.ListNodes("", memory.NodeEpisode, 20, 0)
	if err != nil {
		return fmt.Errorf("list episodes: %w", err)
	}

	newProfile := buildStructuredProfile(facts, prefs, patterns, episodes)

	// Write the backup before touching the live profile.
	if len(currentProfile) > 0 {
		backupPath := p.profilePath + ".bak"
		if err := os.WriteFile(backupPath, currentProfile, 0o644); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}

	if err := os.WriteFile(p.profilePath, []byte(newProfile+"\n"), 0o644); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	p.lastRevision = time.Now()
	p.logger.Info("profile revised",
		"facts", len(facts),
		"preferences", len(prefs),
		"patterns", len(patterns),
		"episodes", len(episodes),
		"profile_len", len(newProfile),
	)

	return nil
}

// buildStructuredProfile assembles a Markdown profile from graph query results.
// It organises nodes into four sections: Identity, Preferences, Behavioral
// Patterns, and Communication Notes. Output is capped at ~8000 characters
// (~2000 tokens at 4 chars/token).
func buildStructuredProfile(facts, prefs, patterns, episodes []memory.Node) string {
	var b strings.Builder

	identityTags := map[string]bool{
		"name": true, "birthday": true, "age": true, "location": true,
		"occupation": true, "family": true, "nationality": true, "language": true, "identity": true,
	}

	b.WriteString("## Identity\n")
	for _, n := range facts {
		if hasAnyTag(n.Tags, identityTags) {
			b.WriteString("- " + n.Title)
			if n.Summary != "" {
				b.WriteString(": " + n.Summary)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString("## Preferences\n")
	groups := groupByTopTag(prefs)
	for tag, nodes := range groups {
		b.WriteString("### " + tag + "\n")
		for _, n := range nodes {
			b.WriteString("- " + n.Title)
			if n.Summary != "" {
				b.WriteString(": " + n.Summary)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	if len(patterns) > 0 {
		b.WriteString("## Behavioral Patterns\n")
		for _, n := range patterns {
			b.WriteString("- " + n.Title)
			if n.Summary != "" {
				b.WriteString(": " + n.Summary)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(episodes) > 0 {
		b.WriteString("## Communication Notes\n")
		for _, n := range episodes {
			b.WriteString("- " + n.Title)
			if n.Summary != "" {
				b.WriteString(": " + n.Summary)
			}
			b.WriteString("\n")
		}
	}

	// Token budget enforcement (~2000 tokens at 4 chars/token = 8000 chars).
	result := b.String()
	for len(result) > 8000 {
		lastNewline := strings.LastIndex(result, "\n")
		if lastNewline <= 0 {
			break
		}
		result = result[:lastNewline]
	}

	return result
}

func hasAnyTag(tags []string, allowed map[string]bool) bool {
	for _, t := range tags {
		if allowed[strings.ToLower(t)] {
			return true
		}
	}
	return false
}

func groupByTopTag(nodes []memory.Node) map[string][]memory.Node {
	groups := make(map[string][]memory.Node)
	for _, n := range nodes {
		tag := "other"
		if len(n.Tags) > 0 {
			tag = strings.ToLower(n.Tags[0])
		}
		groups[tag] = append(groups[tag], n)
	}
	return groups
}
