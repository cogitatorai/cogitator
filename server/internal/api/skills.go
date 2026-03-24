package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/skills"
)

func (r *Router) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	nodes, err := r.skills.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (r *Router) handleSearchSkills(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	result, err := r.skills.Search(req.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleSkillDetail(w http.ResponseWriter, req *http.Request) {
	slug := req.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "missing slug")
		return
	}

	detail, err := r.skills.GetSkillDetail(req.Context(), slug)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch skill detail: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (r *Router) handleInstallSkill(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	var meta skills.SkillMeta
	if err := json.NewDecoder(req.Body).Decode(&meta); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if meta.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	meta.Slug = skills.ParseSlug(meta.Slug)

	nodeID, err := r.skills.Install(req.Context(), meta)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("installation failed: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"node_id": nodeID,
		"name":    meta.DisplayName,
	})
}

func (r *Router) handleReadSkillContent(w http.ResponseWriter, req *http.Request) {
	nodeID := req.PathValue("id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "missing node ID")
		return
	}
	content, err := r.skills.ReadSkillRaw(nodeID)
	if err != nil {
		if errors.Is(err, skills.ErrSkillFileNotFound) {
			writeError(w, http.StatusNotFound, "Skill file is missing from disk. The database references this skill but the SKILL.md file was not found. Try re-importing or re-installing the skill.")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read skill content: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (r *Router) handleUpdateSkill(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	nodeID := req.PathValue("id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "missing node ID")
		return
	}
	var body struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	node, err := r.skills.UpdateSkill(nodeID, body.Title, body.Summary, body.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("update skill: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, node)
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a human-readable name to a lowercase hyphenated slug.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// parseFrontmatter extracts YAML frontmatter fields from SKILL.md content.
// Returns the name and description found in the frontmatter.
func parseFrontmatter(content string) (name, description string) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", ""
	}
	fm := content[3 : 3+end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(k) {
			case "name":
				name = strings.TrimSpace(v)
			case "description":
				description = strings.TrimSpace(v)
			}
		}
	}
	return name, description
}

func (r *Router) handleImportSkill(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	name, description := parseFrontmatter(content)
	if name == "" {
		writeError(w, http.StatusBadRequest, "SKILL.md must have YAML frontmatter with a 'name' field")
		return
	}

	slug := slugify(name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "could not derive slug from skill name")
		return
	}

	nodeID, err := r.skills.CreateSkill(slug, name, description, content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("import failed: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"node_id": nodeID,
		"name":    name,
		"slug":    slug,
	})
}

func (r *Router) handleUninstallSkill(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	nodeID := req.PathValue("id")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "missing node ID")
		return
	}

	if err := r.skills.Uninstall(nodeID); err != nil {
		writeError(w, http.StatusInternalServerError, "uninstall failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
