package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SkillMeta represents a skill entry returned by the ClawHub v1 search API.
type SkillMeta struct {
	Slug        string  `json:"slug"`
	DisplayName string  `json:"displayName"`
	Summary     string  `json:"summary"`
	Version     string  `json:"version"`
	Score       float64 `json:"score,omitempty"`
	UpdatedAt   int64   `json:"updatedAt,omitempty"`
}

// SearchResult holds the response envelope from the ClawHub v1 search endpoint.
type SearchResult struct {
	Results []SkillMeta `json:"results"`
}

// SkillZipMeta represents the _meta.json file bundled inside a downloaded skill ZIP.
type SkillZipMeta struct {
	OwnerID     string `json:"ownerId"`
	Slug        string `json:"slug"`
	Version     string `json:"version"`
	PublishedAt int64  `json:"publishedAt"`
}

// HTTPClient is an interface for making HTTP requests (allows testing with mock implementations).
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ClawHub is a client for the ClawHub skill registry.
type ClawHub struct {
	baseURL string
	client  HTTPClient
}

// NewClawHub creates a new ClawHub client. If baseURL is empty, it defaults to
// "https://clawhub.ai". If client is nil, http.DefaultClient is used.
func NewClawHub(baseURL string, client HTTPClient) *ClawHub {
	if baseURL == "" {
		baseURL = "https://clawhub.ai"
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &ClawHub{baseURL: baseURL, client: client}
}

// Search queries the ClawHub v1 search endpoint and returns up to 10 matching skills.
func (c *ClawHub) Search(ctx context.Context, query string) (*SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", "100")

	endpoint := fmt.Sprintf("%s/api/v1/search?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("clawhub search: build request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clawhub search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub search: unexpected status %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("clawhub search: decode response: %w", err)
	}
	return &result, nil
}

// SkillDetail holds the full detail response from GET /api/v1/skills/{slug}.
type SkillDetail struct {
	Skill struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Summary     string `json:"summary"`
		Tags        map[string]string `json:"tags"`
		Stats       struct {
			Downloads       int `json:"downloads"`
			InstallsAllTime int `json:"installsAllTime"`
			InstallsCurrent int `json:"installsCurrent"`
			Stars           int `json:"stars"`
			Versions        int `json:"versions"`
			Comments        int `json:"comments"`
		} `json:"stats"`
		CreatedAt int64 `json:"createdAt"`
		UpdatedAt int64 `json:"updatedAt"`
	} `json:"skill"`
	LatestVersion struct {
		Version   string `json:"version"`
		Changelog string `json:"changelog"`
		CreatedAt int64  `json:"createdAt"`
	} `json:"latestVersion"`
	Owner struct {
		Handle      string `json:"handle"`
		DisplayName string `json:"displayName"`
	} `json:"owner"`
}

// GetSkillDetail fetches the full detail for a single skill by slug.
func (c *ClawHub) GetSkillDetail(ctx context.Context, slug string) (*SkillDetail, error) {
	endpoint := fmt.Sprintf("%s/api/v1/skills/%s", c.baseURL, url.PathEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("clawhub skill detail: build request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clawhub skill detail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawhub skill detail: unexpected status %d", resp.StatusCode)
	}

	var detail SkillDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("clawhub skill detail: decode: %w", err)
	}
	return &detail, nil
}

// DownloadSkill fetches the ZIP for the given slug from the ClawHub v1 download
// endpoint, extracts SKILL.md and _meta.json from the archive, and returns both.
func (c *ClawHub) DownloadSkill(ctx context.Context, slug string) (skillContent []byte, meta *SkillZipMeta, err error) {
	params := url.Values{}
	params.Set("slug", slug)

	endpoint := fmt.Sprintf("%s/api/v1/download?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("clawhub download %q: build request: %w", slug, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("clawhub download %q: %w", slug, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("clawhub download %q: unexpected status %d", slug, resp.StatusCode)
	}

	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("clawhub download %q: read body: %w", slug, err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, nil, fmt.Errorf("clawhub download %q: open zip: %w", slug, err)
	}

	for _, f := range zr.File {
		switch f.Name {
		case "SKILL.md":
			skillContent, err = readZipFile(f)
			if err != nil {
				return nil, nil, fmt.Errorf("clawhub download %q: read SKILL.md: %w", slug, err)
			}
		case "_meta.json":
			raw, err := readZipFile(f)
			if err != nil {
				return nil, nil, fmt.Errorf("clawhub download %q: read _meta.json: %w", slug, err)
			}
			meta = new(SkillZipMeta)
			if err := json.Unmarshal(raw, meta); err != nil {
				return nil, nil, fmt.Errorf("clawhub download %q: decode _meta.json: %w", slug, err)
			}
		}
	}

	if skillContent == nil {
		return nil, nil, fmt.Errorf("clawhub download %q: SKILL.md not found in archive", slug)
	}
	if meta == nil {
		return nil, nil, fmt.Errorf("clawhub download %q: _meta.json not found in archive", slug)
	}

	const maxSkillSize = 64 * 1024 // 64KB
	if len(skillContent) > maxSkillSize {
		return nil, nil, fmt.Errorf("clawhub download %q: SKILL.md exceeds size limit (%d bytes, max %d)", slug, len(skillContent), maxSkillSize)
	}

	return skillContent, meta, nil
}

// ParseSlug extracts a skill slug from a ClawHub URL (e.g.
// "https://clawhub.ai/skills/weather") or returns the input unchanged
// if it is not a recognizable URL.
func ParseSlug(input string) string {
	u, err := url.Parse(input)
	if err != nil || u.Scheme == "" {
		return input // not a URL, treat as slug
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "skills" && parts[1] != "" {
		return parts[1]
	}
	return input
}

// readZipFile reads the full contents of a single file entry from a ZIP archive.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
