package tools

import (
	"context"

	"github.com/cogitatorai/cogitator/server/internal/security"
	"github.com/cogitatorai/cogitator/server/internal/skills"
)

// SkillManagerAdapter adapts skills.Manager to the SkillManager interface
// so the executor can search, install, and read skills without depending
// on the full skills package API.
type SkillManagerAdapter struct {
	Manager *skills.Manager
}

func (a *SkillManagerAdapter) Search(ctx context.Context, query string) ([]map[string]any, error) {
	result, err := a.Manager.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	// Build a slug lookup of already-installed skills so search results can
	// indicate which ones the user already has.
	installed := make(map[string]string) // slug -> node_id
	if nodes, err := a.Manager.List(); err == nil {
		for _, n := range nodes {
			if slug := skills.SlugFromPath(n.SkillPath); slug != "" {
				installed[slug] = n.ID
			}
		}
	}

	out := make([]map[string]any, len(result.Results))
	for i, s := range result.Results {
		entry := map[string]any{
			"slug":         s.Slug,
			"display_name": s.DisplayName,
			"summary":      s.Summary,
			"version":      s.Version,
			"installed":    false,
		}
		if nodeID, ok := installed[s.Slug]; ok {
			entry["installed"] = true
			entry["node_id"] = nodeID
		}
		out[i] = entry
	}
	return out, nil
}

func (a *SkillManagerAdapter) Install(ctx context.Context, slug string, force bool) (map[string]any, error) {
	slug = skills.ParseSlug(slug)

	// Resolve full metadata (display name, summary) for the memory node.
	// Try search first (cheap, single call), then fall back to detail endpoint.
	meta := skills.SkillMeta{Slug: slug}
	result, err := a.Manager.Search(ctx, slug)
	if err == nil {
		for _, s := range result.Results {
			if s.Slug == slug {
				meta = s
				break
			}
		}
	}
	if meta.DisplayName == "" {
		if detail, err := a.Manager.GetSkillDetail(ctx, slug); err == nil {
			meta.DisplayName = detail.Skill.DisplayName
			if meta.Summary == "" {
				meta.Summary = detail.Skill.Summary
			}
		}
	}

	// Two-phase install: download first, scan, then commit only if safe (or forced).
	content, zipMeta, err := a.Manager.Download(ctx, meta)
	if err != nil {
		return nil, err
	}

	scan := skills.ScanContent(content)

	// Build warnings once (reused by both security and domain gates).
	var warnings []map[string]any
	if len(scan.Findings) > 0 {
		warnings = make([]map[string]any, len(scan.Findings))
		for i, f := range scan.Findings {
			warnings[i] = map[string]any{
				"severity":    string(f.Severity),
				"description": f.Description,
				"line":        f.Line,
				"snippet":     f.Snippet,
			}
		}
	}

	// If the scan found high-severity issues and the user hasn't approved, gate the install.
	if scan.Blocked && !force {
		return map[string]any{
			"status":   "review_required",
			"slug":     slug,
			"warnings": warnings,
			"message":  "This skill contains suspicious patterns. Present the warnings to the user and only retry with force=true if they explicitly approve.",
		}, nil
	}

	// Check for domains not yet in the allowlist. Gate the install so the
	// agent can present them to the user before auto-allowlisting.
	domains := security.ExtractDomainsFromText(string(content))
	if newDomains := a.Manager.NewDomains(domains); len(newDomains) > 0 && !force {
		resp := map[string]any{
			"status":           "domain_approval_required",
			"slug":             slug,
			"required_domains": newDomains,
			"message":          "This skill needs network access to the following domains. Present them to the user and only retry with force=true after they approve.",
		}
		if len(warnings) > 0 {
			resp["warnings"] = warnings
		}
		return resp, nil
	}

	nodeID, err := a.Manager.InstallFromContent(ctx, meta, content, zipMeta)
	if err != nil {
		return nil, err
	}

	resp := map[string]any{
		"node_id":      nodeID,
		"slug":         slug,
		"display_name": meta.DisplayName,
		"status":       "installed",
	}

	// Include any medium-severity warnings so the LLM can inform the user.
	if len(scan.Findings) > 0 {
		warnings := make([]map[string]any, len(scan.Findings))
		for i, f := range scan.Findings {
			warnings[i] = map[string]any{
				"severity":    string(f.Severity),
				"description": f.Description,
				"line":        f.Line,
				"snippet":     f.Snippet,
			}
		}
		resp["warnings"] = warnings
	}

	return resp, nil
}

func (a *SkillManagerAdapter) CreateSkill(slug, name, summary, content string) (map[string]any, error) {
	nodeID, err := a.Manager.CreateSkill(slug, name, summary, content)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"node_id": nodeID,
		"slug":    slug,
		"name":    name,
		"origin":  "learned",
		"status":  "created",
	}, nil
}

func (a *SkillManagerAdapter) List() ([]map[string]any, error) {
	nodes, err := a.Manager.List()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(nodes))
	for i, n := range nodes {
		out[i] = map[string]any{
			"node_id": n.ID,
			"title":   n.Title,
			"summary": n.Summary,
			"version": n.Version,
			"origin":  n.Origin,
		}
	}
	return out, nil
}

func (a *SkillManagerAdapter) ReadSkill(nodeID string) (string, error) {
	return a.Manager.ReadSkill(nodeID)
}

func (a *SkillManagerAdapter) UpdateSkill(nodeID, title, summary, content string) (map[string]any, error) {
	node, err := a.Manager.UpdateSkill(nodeID, title, summary, content)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"node_id": node.ID,
		"title":   node.Title,
		"status":  "updated",
	}, nil
}
