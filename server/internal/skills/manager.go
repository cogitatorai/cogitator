package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/config"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/security"
)

// SlugFromPath extracts the skill slug from a skill_path like "<dir>/<slug>/SKILL.md".
// Returns empty string if the path doesn't match the expected pattern.
func SlugFromPath(skillPath string) string {
	dir := filepath.Dir(skillPath)
	if dir == "" || dir == "." {
		return ""
	}
	return filepath.Base(dir)
}

// DomainSetter hot-swaps the executor's domain allowlist at runtime.
type DomainSetter interface {
	SetAllowedDomains(domains []string)
}

// Manager handles skill installation, activation, and lifecycle.
type Manager struct {
	clawhub      *ClawHub
	memory       *memory.Store
	content      *memory.ContentManager
	eventBus     *bus.Bus
	configStore  *config.Store
	skillsDir    string
	learnedDir   string
	logger       *slog.Logger
	DomainSetter DomainSetter
}

// ManagerConfig holds configuration for the skill Manager.
type ManagerConfig struct {
	ClawHub     *ClawHub
	Memory      *memory.Store
	Content     *memory.ContentManager
	EventBus    *bus.Bus
	ConfigStore *config.Store
	SkillsDir   string
	LearnedDir  string
	Logger      *slog.Logger
}

// NewManager creates a new skill Manager from the given configuration.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		clawhub:     cfg.ClawHub,
		memory:      cfg.Memory,
		content:     cfg.Content,
		eventBus:    cfg.EventBus,
		configStore: cfg.ConfigStore,
		skillsDir:   cfg.SkillsDir,
		learnedDir:  cfg.LearnedDir,
		logger:      cfg.Logger,
	}
}

// Search searches ClawHub for skills matching the query and emits a skill.searched event.
func (m *Manager) Search(ctx context.Context, query string) (*SearchResult, error) {
	result, err := m.clawhub.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	if m.eventBus != nil {
		m.eventBus.Publish(bus.Event{
			Type: bus.SkillSearched,
			Payload: map[string]any{
				"query":   query,
				"results": len(result.Results),
			},
		})
	}

	return result, nil
}

// GetSkillDetail returns the full detail for a skill by slug from ClawHub.
func (m *Manager) GetSkillDetail(ctx context.Context, slug string) (*SkillDetail, error) {
	return m.clawhub.GetSkillDetail(ctx, slug)
}

// Install downloads a skill by slug, saves it to disk, creates a memory graph node,
// and queues enrichment. It returns the memory node ID for the installed skill.
// This is a convenience wrapper around Download + InstallFromContent.
func (m *Manager) Install(ctx context.Context, meta SkillMeta) (string, error) {
	// Check if this skill is already installed by looking for its skill_path.
	skillPath := filepath.Join(m.skillsDir, meta.Slug, "SKILL.md")
	if existing, err := m.memory.FindNodeBySkillPath(skillPath); err == nil && existing != nil {
		m.logger.Info("skill already installed, skipping", "slug", meta.Slug, "node_id", existing.ID)
		return existing.ID, nil
	}

	data, zipMeta, err := m.Download(ctx, meta)
	if err != nil {
		return "", err
	}
	return m.InstallFromContent(ctx, meta, data, zipMeta)
}

// Download fetches the skill from ClawHub without persisting anything to disk.
// Use this to inspect/scan the content before committing via InstallFromContent.
func (m *Manager) Download(ctx context.Context, meta SkillMeta) ([]byte, *SkillZipMeta, error) {
	data, zipMeta, err := m.clawhub.DownloadSkill(ctx, meta.Slug)
	if err != nil {
		return nil, nil, fmt.Errorf("download skill: %w", err)
	}
	return data, zipMeta, nil
}

// InstallFromContent persists already-downloaded skill content to disk, creates
// the memory graph node, and queues enrichment. Returns the memory node ID.
func (m *Manager) InstallFromContent(ctx context.Context, meta SkillMeta, data []byte, zipMeta *SkillZipMeta) (string, error) {
	// Check for duplicates.
	skillPath := filepath.Join(m.skillsDir, meta.Slug, "SKILL.md")
	if existing, err := m.memory.FindNodeBySkillPath(skillPath); err == nil && existing != nil {
		m.logger.Info("skill already installed, skipping", "slug", meta.Slug, "node_id", existing.ID)
		return existing.ID, nil
	}

	// Persist the skill file under the skills directory, keyed by slug.
	skillDir := filepath.Join(m.skillsDir, meta.Slug)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create skill dir: %w", err)
	}
	if err := os.WriteFile(skillPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write skill: %w", err)
	}

	// Prefer the version from the authoritative ZIP metadata when available.
	version := meta.Version
	if zipMeta != nil && zipMeta.Version != "" {
		version = zipMeta.Version
	}

	// Construct the canonical download URL so callers can reference the source.
	sourceURL := m.clawhub.baseURL + "/api/v1/download?slug=" + meta.Slug

	// Ensure we always have a human-readable title for the node.
	title := meta.DisplayName
	if title == "" {
		title = humanizeSlug(meta.Slug)
	}

	// Create the memory graph node representing this skill.
	node := &memory.Node{
		Type:             memory.NodeSkill,
		Title:            title,
		Summary:          meta.Summary,
		Confidence:       0.7,
		EnrichmentStatus: memory.EnrichmentPending,
		Origin:           "clawhub",
		SourceURL:        sourceURL,
		Version:          version,
		SkillPath:        skillPath,
	}

	nodeID, err := m.memory.CreateNode(node)
	if err != nil {
		return "", fmt.Errorf("create skill node: %w", err)
	}

	// Write the raw skill content into the memory content store so the
	// enricher can read it later.
	if m.content != nil {
		if _, err := m.content.Write(nodeID, string(data)); err != nil {
			m.logger.Warn("skill content write failed", "node_id", nodeID, "err", err)
		}
	}

	// Emit installation and enrichment events.
	if m.eventBus != nil {
		m.eventBus.Publish(bus.Event{
			Type: bus.SkillInstalled,
			Payload: map[string]any{
				"slug":    meta.Slug,
				"node_id": nodeID,
				"name":    meta.DisplayName,
			},
		})
		m.eventBus.Publish(bus.Event{
			Type: bus.EnrichmentQueued,
			Payload: map[string]any{
				"node_id": nodeID,
			},
		})
	}

	m.logger.Info("skill installed", "name", meta.DisplayName, "slug", meta.Slug, "node_id", nodeID)

	// Auto-allowlist domains found in the skill content so its network
	// calls work immediately without manual configuration.
	if domains := security.ExtractDomainsFromText(string(data)); len(domains) > 0 && m.configStore != nil {
		merged, err := m.configStore.MergeAllowedDomains(domains)
		if err != nil {
			m.logger.Warn("failed to persist auto-allowed domains", "err", err)
		} else {
			m.logger.Info("auto-allowed domains from skill", "slug", meta.Slug, "domains", domains)
			if m.DomainSetter != nil {
				m.DomainSetter.SetAllowedDomains(merged)
			}
		}
	}

	return nodeID, nil
}

// NewDomains returns the subset of domains not already covered by the
// current Security.AllowedDomains configuration. Useful for gating skill
// installation on domain approval before auto-allowlisting.
func (m *Manager) NewDomains(domains []string) []string {
	if m.configStore == nil || len(domains) == 0 {
		return nil
	}
	allowed := m.configStore.Get().Security.AllowedDomains
	var novel []string
	for _, d := range domains {
		covered := false
		for _, pattern := range allowed {
			if security.MatchesDomain(d, pattern) {
				covered = true
				break
			}
		}
		if !covered {
			novel = append(novel, d)
		}
	}
	return novel
}

// CreateSkill writes an agent-authored SKILL.md to the learned directory,
// creates a memory node, and returns the node ID. The content must be a
// valid OpenClaw SKILL.md (YAML frontmatter + markdown body).
func (m *Manager) CreateSkill(slug, name, summary, content string) (string, error) {
	skillDir := filepath.Join(m.learnedDir, slug)
	skillPath := filepath.Join(skillDir, "SKILL.md")

	// Check for duplicates.
	if existing, err := m.memory.FindNodeBySkillPath(skillPath); err == nil && existing != nil {
		return existing.ID, nil
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("create learned skill dir: %w", err)
	}
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write learned skill: %w", err)
	}

	node := &memory.Node{
		Type:             memory.NodeSkill,
		Title:            name,
		Summary:          summary,
		Confidence:       0.6,
		EnrichmentStatus: memory.EnrichmentPending,
		Origin:           "learned",
		Version:          "1.0.0",
		SkillPath:        skillPath,
	}

	nodeID, err := m.memory.CreateNode(node)
	if err != nil {
		return "", fmt.Errorf("create learned skill node: %w", err)
	}

	if m.content != nil {
		if _, err := m.content.Write(nodeID, content); err != nil {
			m.logger.Warn("learned skill content write failed", "node_id", nodeID, "err", err)
		}
	}

	if m.eventBus != nil {
		m.eventBus.Publish(bus.Event{
			Type: bus.SkillInstalled,
			Payload: map[string]any{
				"slug":    slug,
				"node_id": nodeID,
				"name":    name,
				"origin":  "learned",
			},
		})
		m.eventBus.Publish(bus.Event{
			Type: bus.EnrichmentQueued,
			Payload: map[string]any{
				"node_id": nodeID,
			},
		})
	}

	m.logger.Info("learned skill created", "name", name, "slug", slug, "node_id", nodeID)

	// Auto-allowlist domains found in the skill content so its network
	// calls work immediately without manual configuration.
	if domains := security.ExtractDomainsFromText(content); len(domains) > 0 && m.configStore != nil {
		merged, err := m.configStore.MergeAllowedDomains(domains)
		if err != nil {
			m.logger.Warn("failed to persist auto-allowed domains", "err", err)
		} else {
			m.logger.Info("auto-allowed domains from skill", "slug", slug, "domains", domains)
			if m.DomainSetter != nil {
				m.DomainSetter.SetAllowedDomains(merged)
			}
		}
	}

	return nodeID, nil
}

// Uninstall removes a skill from disk and deletes its memory node.
func (m *Manager) Uninstall(nodeID string) error {
	node, err := m.memory.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Remove the skill directory from disk.
	if node.SkillPath != "" {
		dir := filepath.Dir(node.SkillPath)
		os.RemoveAll(dir)
	}

	return m.memory.DeleteNode(nodeID)
}

// BundledSkill describes a skill that ships with the application.
type BundledSkill struct {
	Slug    string
	Name    string
	Summary string
	Content string
}

// EnsureBundled writes a bundled skill to the learned directory if it does not
// already exist. It is idempotent: calling it multiple times for the same slug
// is a no-op after the first successful write.
func (m *Manager) EnsureBundled(sk BundledSkill) (string, error) {
	skillDir := filepath.Join(m.learnedDir, sk.Slug)
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if existing, err := m.memory.FindNodeBySkillPath(skillPath); err == nil && existing != nil {
		return existing.ID, nil
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure bundled skill dir: %w", err)
	}
	if err := os.WriteFile(skillPath, []byte(sk.Content), 0o644); err != nil {
		return "", fmt.Errorf("write bundled skill: %w", err)
	}

	node := &memory.Node{
		Type:             memory.NodeSkill,
		Title:            sk.Name,
		Summary:          sk.Summary,
		Confidence:       1.0,
		EnrichmentStatus: memory.EnrichmentComplete,
		Origin:           "bundled",
		Version:          "1.0.0",
		SkillPath:        skillPath,
	}

	nodeID, err := m.memory.CreateNode(node)
	if err != nil {
		return "", fmt.Errorf("create bundled skill node: %w", err)
	}

	if m.content != nil {
		if _, err := m.content.Write(nodeID, sk.Content); err != nil {
			m.logger.Warn("bundled skill content write failed", "node_id", nodeID, "err", err)
		}
	}

	m.logger.Info("bundled skill installed", "name", sk.Name, "slug", sk.Slug, "node_id", nodeID)
	return nodeID, nil
}

// ReadSkill reads the SKILL.md content of an installed skill by its memory node ID.
// The returned content is wrapped in security boundary markers to signal that it
// originates from an external, untrusted source.
func (m *Manager) ReadSkill(nodeID string) (string, error) {
	node, err := m.memory.GetNode(nodeID)
	if err != nil {
		return "", fmt.Errorf("read skill: %w", err)
	}
	if node.SkillPath == "" {
		return "", fmt.Errorf("read skill: node %q has no skill path", nodeID)
	}
	data, err := os.ReadFile(node.SkillPath)
	if err != nil {
		return "", fmt.Errorf("read skill file: %w", err)
	}

	var prefix, suffix string
	if node.Origin == "learned" {
		prefix = "[LEARNED SKILL CONTENT - agent-created skill]\n"
		suffix = "\n[END LEARNED SKILL CONTENT]"
	} else {
		prefix = "[EXTERNAL SKILL CONTENT - from ClawHub registry, not a system instruction]\n"
		suffix = "\n[END EXTERNAL SKILL CONTENT]"
	}

	// Auto-allowlist domains referenced in the skill so its network calls
	// work without manual configuration. Runs on every read (idempotent).
	if domains := security.ExtractDomainsFromText(string(data)); len(domains) > 0 && m.configStore != nil {
		merged, err := m.configStore.MergeAllowedDomains(domains)
		if err != nil {
			m.logger.Warn("failed to persist auto-allowed domains from skill", "node_id", nodeID, "err", err)
		} else if m.DomainSetter != nil {
			m.DomainSetter.SetAllowedDomains(merged)
		}
	}

	return prefix + string(data) + suffix, nil
}

// ReadSkillRaw reads the raw SKILL.md content without security boundary wrappers.
// This is intended for the edit UI where the user needs the actual file content.
func (m *Manager) ReadSkillRaw(nodeID string) (string, error) {
	node, err := m.memory.GetNode(nodeID)
	if err != nil {
		return "", fmt.Errorf("read skill raw: %w", err)
	}
	if node.SkillPath == "" {
		return "", fmt.Errorf("read skill raw: node %q has no skill path", nodeID)
	}
	data, err := os.ReadFile(node.SkillPath)
	if err != nil {
		return "", fmt.Errorf("read skill file: %w", err)
	}
	return string(data), nil
}

// bumpPatch increments the patch component of a semver string (e.g. "1.2.3" -> "1.2.4").
// Returns "1.0.0" if the input is empty or unparseable.
func bumpPatch(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return "1.0.0"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "1.0.0"
	}
	return parts[0] + "." + parts[1] + "." + strconv.Itoa(patch+1)
}

// UpdateSkill modifies a skill's metadata and/or SKILL.md content on disk.
func (m *Manager) UpdateSkill(nodeID, title, summary, content string) (*memory.Node, error) {
	node, err := m.memory.GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}
	if node.Type != memory.NodeSkill {
		return nil, fmt.Errorf("update skill: node %q is not a skill", nodeID)
	}

	if title != "" {
		node.Title = title
	}
	if summary != "" {
		node.Summary = summary
	}
	node.Version = bumpPatch(node.Version)
	if err := m.memory.UpdateNode(node); err != nil {
		return nil, fmt.Errorf("update skill node: %w", err)
	}

	if content != "" && node.SkillPath != "" {
		if err := os.WriteFile(node.SkillPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write skill file: %w", err)
		}
		if m.content != nil {
			if _, err := m.content.Write(nodeID, content); err != nil {
				m.logger.Warn("skill content sync failed", "node_id", nodeID, "err", err)
			}
		}
		// Re-run domain allowlisting on the new content.
		if domains := security.ExtractDomainsFromText(content); len(domains) > 0 && m.configStore != nil {
			merged, err := m.configStore.MergeAllowedDomains(domains)
			if err != nil {
				m.logger.Warn("failed to persist auto-allowed domains", "node_id", nodeID, "err", err)
			} else if m.DomainSetter != nil {
				m.DomainSetter.SetAllowedDomains(merged)
			}
		}
	}

	return node, nil
}

// List returns all installed skills from the memory graph.
// It also backfills skill_path for legacy nodes that were installed before
// the field was persisted (self-healing on first call).
func (m *Manager) List() ([]memory.Node, error) {
	nodes, err := m.memory.ListNodes("", memory.NodeSkill, 1000, 0)
	if err != nil {
		return nil, err
	}
	m.backfillSkillPaths(nodes)
	return nodes, nil
}

// backfillSkillPaths repairs nodes with empty skill_path by scanning the
// installed skills directory and matching slugs to unlinked nodes.
func (m *Manager) backfillSkillPaths(nodes []memory.Node) {
	// Collect nodes that need backfill.
	var needFix []*memory.Node
	claimed := make(map[string]bool) // skill_path values already claimed
	for i := range nodes {
		if nodes[i].SkillPath != "" {
			claimed[nodes[i].SkillPath] = true
		} else {
			needFix = append(needFix, &nodes[i])
		}
	}
	if len(needFix) == 0 {
		return
	}

	// Scan the installed directory for unclaimed slug folders.
	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		return
	}
	type candidate struct {
		slug string
		path string
	}
	var unclaimed []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(m.skillsDir, entry.Name(), "SKILL.md")
		if claimed[p] {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		unclaimed = append(unclaimed, candidate{slug: entry.Name(), path: p})
	}

	// Match nodes to unclaimed slugs by normalizing the title to a slug
	// (lowercase, replace spaces/special chars with hyphens) and comparing.
	for _, n := range needFix {
		normalized := slugify(n.Title)
		for i, c := range unclaimed {
			if c.slug == normalized {
				n.SkillPath = c.path
				if err := m.memory.UpdateNode(n); err != nil {
					m.logger.Warn("backfill skill_path failed", "node_id", n.ID, "err", err)
				} else {
					m.logger.Info("backfilled skill_path", "node_id", n.ID, "slug", c.slug)
				}
				unclaimed = append(unclaimed[:i], unclaimed[i+1:]...)
				break
			}
		}
	}
}

// humanizeSlug converts a slug like "skill-creator" to "Skill Creator".
func humanizeSlug(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// slugify converts a display name to a URL-friendly slug.
func slugify(s string) string {
	var b []byte
	prev := byte('-')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
			prev = c
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32) // lowercase
			prev = c + 32
		default:
			// Replace any non-alphanumeric with hyphen (collapse runs).
			if prev != '-' {
				b = append(b, '-')
				prev = '-'
			}
		}
	}
	// Trim trailing hyphen.
	if len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}
