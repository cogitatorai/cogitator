package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// ProfilerConfig holds the dependencies and configuration for the Profiler worker.
type ProfilerConfig struct {
	Memory         *memory.Store
	Provider       provider.Provider
	EventBus       *bus.Bus
	Model          string
	ProfilePath    string
	Logger         *slog.Logger
	RegenThreshold int // number of new memories before profile regeneration (default 5)
}

// Profiler listens for ProfileRevisionDue events, queries recent behavioral
// evidence from the memory graph, and calls an LLM to revise the behavioral
// profile on disk. It also counts EnrichmentQueued events and triggers
// revision when the count reaches regenThreshold.
type Profiler struct {
	mu             sync.RWMutex
	memory         *memory.Store
	provider       provider.Provider
	eventBus       *bus.Bus
	model          string
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
		provider:       cfg.Provider,
		eventBus:       cfg.EventBus,
		model:          cfg.Model,
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

// SetProvider hot-swaps the LLM provider and model used for profile revision.
func (p *Profiler) SetProvider(prov provider.Provider, model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provider = prov
	if model != "" {
		p.model = model
	}
}

// revise loads the current profile, gathers recent evidence, calls the LLM,
// and writes the revised profile to disk (backing up the previous version first).
func (p *Profiler) revise(ctx context.Context, _ bus.Event) error {
	p.mu.RLock()
	prov := p.provider
	model := p.model
	p.mu.RUnlock()

	if prov == nil {
		p.logger.Warn("profile revision skipped: no provider configured")
		return nil
	}

	// Load the current profile. If the file does not exist yet, start with an empty profile.
	currentProfile, err := os.ReadFile(p.profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read profile: %w", err)
	}

	// Collect evidence nodes updated since the last revision.
	evidence, err := p.recentEvidence()
	if err != nil {
		return fmt.Errorf("collect evidence: %w", err)
	}

	prompt := buildProfileRevisionPrompt(string(currentProfile), evidence)

	msgs := []provider.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := prov.Chat(ctx, msgs, nil, model, nil)
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}

	newProfile := strings.TrimSpace(resp.Content)

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
		"evidence_nodes", len(evidence),
		"response_len", len(newProfile),
	)

	return nil
}

// recentEvidence returns nodes of type Episode, Preference, and Pattern that
// were updated after the last revision timestamp.
func (p *Profiler) recentEvidence() ([]memory.Node, error) {
	types := []memory.NodeType{
		memory.NodeEpisode,
		memory.NodePreference,
		memory.NodePattern,
	}

	const maxPerType = 100

	var evidence []memory.Node
	for _, nt := range types {
		nodes, err := p.memory.ListNodes("", nt, maxPerType, 0)
		if err != nil {
			return nil, fmt.Errorf("list %s nodes: %w", nt, err)
		}
		for _, n := range nodes {
			if p.lastRevision.IsZero() || n.UpdatedAt.After(p.lastRevision) {
				evidence = append(evidence, n)
			}
		}
	}

	return evidence, nil
}

// buildProfileRevisionPrompt constructs the prompt sent to the LLM.
func buildProfileRevisionPrompt(currentProfile string, evidence []memory.Node) string {
	var sb strings.Builder

	sb.WriteString("You are a behavioral profile revision agent. ")
	sb.WriteString("Below is the current behavioral profile and recent evidence from the knowledge graph.\n\n")

	sb.WriteString("Current Profile:\n")
	if strings.TrimSpace(currentProfile) == "" {
		sb.WriteString("(empty)\n")
	} else {
		sb.WriteString(currentProfile)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	sb.WriteString("Recent Evidence:\n")
	if len(evidence) == 0 {
		sb.WriteString("(no recent evidence)\n")
	} else {
		for _, n := range evidence {
			sb.WriteString(fmt.Sprintf("- [%s] %s (id: %s)", n.Type, n.Title, n.ID))
			if n.Summary != "" {
				sb.WriteString(": ")
				sb.WriteString(n.Summary)
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")

	sb.WriteString("Instructions:\n")
	sb.WriteString("1. Evaluate whether any profile rules need to be added, modified, or removed based on the evidence.\n")
	sb.WriteString("2. If changes are warranted, rewrite the affected sections. Keep evidence links (node IDs) for each rule.\n")
	sb.WriteString("3. If no changes are needed, return the profile unchanged.\n")
	sb.WriteString("4. The profile must stay under 500 tokens. Be concise.\n")
	sb.WriteString("5. Return ONLY the complete profile as Markdown (no explanation, no code fences).\n")

	return sb.String()
}
