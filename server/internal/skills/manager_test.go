package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
)

func testDB(t *testing.T) *database.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// testV1Server returns an httptest.Server that serves v1 search and download endpoints.
func testV1Server(t *testing.T, meta SkillMeta, skillContent string, zipMeta SkillZipMeta) *httptest.Server {
	t.Helper()
	zipData := makeSkillZip(t, skillContent, zipMeta)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(SearchResult{
				Results: []SkillMeta{meta},
			})
		case "/api/v1/download":
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipData)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestManagerInstall(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	contentDir := t.TempDir()
	contentMgr := memory.NewContentManager(contentDir)
	skillsDir := t.TempDir()
	eventBus := bus.New()
	defer eventBus.Close()

	skillContent := "# Git Skill\n\nKnows how to use git.\n"
	meta := SkillMeta{
		Slug:        "git-skill",
		DisplayName: "Git Skill",
		Summary:     "Knows how to use git",
		Version:     "1.0.0",
	}
	zipMeta := SkillZipMeta{
		Slug:    "git-skill",
		Version: "1.0.0",
	}

	srv := testV1Server(t, meta, skillContent, zipMeta)

	// Subscribe to events before install so we capture them.
	installedCh := eventBus.Subscribe(bus.SkillInstalled)
	enrichCh := eventBus.Subscribe(bus.EnrichmentQueued)
	defer eventBus.Unsubscribe(installedCh)
	defer eventBus.Unsubscribe(enrichCh)

	ch := NewClawHub(srv.URL, nil)
	mgr := NewManager(ManagerConfig{
		ClawHub:   ch,
		Memory:    store,
		Content:   contentMgr,
		EventBus:  eventBus,
		SkillsDir: skillsDir,
	})

	nodeID, err := mgr.Install(context.Background(), meta)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	if nodeID == "" {
		t.Fatal("Install() returned empty nodeID")
	}

	// Verify the skill file was written to disk.
	expectedPath := filepath.Join(skillsDir, meta.Slug, "SKILL.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("skill file not found at %s: %v", expectedPath, err)
	}
	if string(data) != skillContent {
		t.Errorf("skill file content = %q, want %q", string(data), skillContent)
	}

	// Verify the memory node was created with correct fields.
	node, err := store.GetNode(nodeID)
	if err != nil {
		t.Fatalf("GetNode() error: %v", err)
	}
	if node.Type != memory.NodeSkill {
		t.Errorf("node.Type = %q, want %q", node.Type, memory.NodeSkill)
	}
	if node.Title != meta.DisplayName {
		t.Errorf("node.Title = %q, want %q", node.Title, meta.DisplayName)
	}
	if node.Origin != "clawhub" {
		t.Errorf("node.Origin = %q, want %q", node.Origin, "clawhub")
	}
	if node.Version != meta.Version {
		t.Errorf("node.Version = %q, want %q", node.Version, meta.Version)
	}
	if node.SkillPath != expectedPath {
		t.Errorf("node.SkillPath = %q, want %q", node.SkillPath, expectedPath)
	}
	if node.Confidence != 0.7 {
		t.Errorf("node.Confidence = %v, want 0.7", node.Confidence)
	}

	// Verify events were emitted.
	select {
	case evt := <-installedCh:
		if evt.Payload["node_id"] != nodeID {
			t.Errorf("SkillInstalled payload node_id = %v, want %q", evt.Payload["node_id"], nodeID)
		}
		if evt.Payload["slug"] != meta.Slug {
			t.Errorf("SkillInstalled payload slug = %v, want %q", evt.Payload["slug"], meta.Slug)
		}
	default:
		t.Error("expected SkillInstalled event, channel empty")
	}

	select {
	case evt := <-enrichCh:
		if evt.Payload["node_id"] != nodeID {
			t.Errorf("EnrichmentQueued payload node_id = %v, want %q", evt.Payload["node_id"], nodeID)
		}
	default:
		t.Error("expected EnrichmentQueued event, channel empty")
	}
}

func TestManagerUninstall(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	skillsDir := t.TempDir()

	skillContent := "# Test Skill\n\nDoes something.\n"
	meta := SkillMeta{
		Slug:        "test-skill",
		DisplayName: "Test Skill",
		Version:     "0.1.0",
	}
	zipMeta := SkillZipMeta{Slug: "test-skill", Version: "0.1.0"}

	srv := testV1Server(t, meta, skillContent, zipMeta)

	ch := NewClawHub(srv.URL, nil)
	mgr := NewManager(ManagerConfig{
		ClawHub:   ch,
		Memory:    store,
		SkillsDir: skillsDir,
	})

	nodeID, err := mgr.Install(context.Background(), meta)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	skillPath := filepath.Join(skillsDir, meta.Slug, "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("expected skill file to exist before uninstall: %s", skillPath)
	}

	if err := mgr.Uninstall(nodeID); err != nil {
		t.Fatalf("Uninstall() error: %v", err)
	}

	// The skill directory should be gone.
	if _, err := os.Stat(filepath.Dir(skillPath)); !os.IsNotExist(err) {
		t.Error("expected skill directory to be removed after uninstall")
	}

	// The memory node should be deleted.
	_, err = store.GetNode(nodeID)
	if err != memory.ErrNotFound {
		t.Errorf("GetNode() after uninstall = %v, want ErrNotFound", err)
	}
}

func TestManagerList(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	skillsDir := t.TempDir()

	meta1 := SkillMeta{Slug: "skill-a", DisplayName: "Skill A", Version: "1.0.0"}
	meta2 := SkillMeta{Slug: "skill-b", DisplayName: "Skill B", Version: "1.0.0"}
	zipMeta1 := SkillZipMeta{Slug: "skill-a", Version: "1.0.0"}
	zipMeta2 := SkillZipMeta{Slug: "skill-b", Version: "1.0.0"}

	srv1 := testV1Server(t, meta1, "# Skill A\n", zipMeta1)
	srv2 := testV1Server(t, meta2, "# Skill B\n", zipMeta2)

	mgr1 := NewManager(ManagerConfig{
		ClawHub:   NewClawHub(srv1.URL, nil),
		Memory:    store,
		SkillsDir: skillsDir,
	})
	mgr2 := NewManager(ManagerConfig{
		ClawHub:   NewClawHub(srv2.URL, nil),
		Memory:    store,
		SkillsDir: skillsDir,
	})

	if _, err := mgr1.Install(context.Background(), meta1); err != nil {
		t.Fatalf("Install skill-a: %v", err)
	}
	if _, err := mgr2.Install(context.Background(), meta2); err != nil {
		t.Fatalf("Install skill-b: %v", err)
	}

	nodes, err := mgr1.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("List() returned %d skills, want 2", len(nodes))
	}
}

func TestManagerSearch(t *testing.T) {
	meta := SkillMeta{
		Slug:        "search-skill",
		DisplayName: "Search Skill",
		Version:     "1.0.0",
	}
	zipMeta := SkillZipMeta{Slug: "search-skill", Version: "1.0.0"}
	srv := testV1Server(t, meta, "content", zipMeta)

	eventBus := bus.New()
	defer eventBus.Close()

	searchedCh := eventBus.Subscribe(bus.SkillSearched)
	defer eventBus.Unsubscribe(searchedCh)

	mgr := NewManager(ManagerConfig{
		ClawHub:   NewClawHub(srv.URL, nil),
		Memory:    memory.NewStore(testDB(t)),
		EventBus:  eventBus,
		SkillsDir: t.TempDir(),
	})

	result, err := mgr.Search(context.Background(), "search")
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("Search() len(Results) = %d, want 1", len(result.Results))
	}
	if result.Results[0].Slug != meta.Slug {
		t.Errorf("Search() Results[0].Slug = %q, want %q", result.Results[0].Slug, meta.Slug)
	}

	select {
	case evt := <-searchedCh:
		if evt.Payload["query"] != "search" {
			t.Errorf("SkillSearched payload query = %v, want %q", evt.Payload["query"], "search")
		}
		if evt.Payload["results"] != 1 {
			t.Errorf("SkillSearched payload results = %v, want 1", evt.Payload["results"])
		}
	default:
		t.Error("expected SkillSearched event, channel empty")
	}
}

func TestManagerReadSkill(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	skillsDir := t.TempDir()

	skillContent := "# Weather\n\nUse curl wttr.in.\n"
	meta := SkillMeta{
		Slug:        "weather",
		DisplayName: "Weather",
		Version:     "1.0.0",
	}
	zipMeta := SkillZipMeta{Slug: "weather", Version: "1.0.0"}

	srv := testV1Server(t, meta, skillContent, zipMeta)

	mgr := NewManager(ManagerConfig{
		ClawHub:   NewClawHub(srv.URL, nil),
		Memory:    store,
		SkillsDir: skillsDir,
	})

	nodeID, err := mgr.Install(context.Background(), meta)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	content, err := mgr.ReadSkill(nodeID)
	if err != nil {
		t.Fatalf("ReadSkill() error: %v", err)
	}

	// Should contain the original skill content wrapped in security markers.
	if !strings.Contains(content, skillContent) {
		t.Errorf("ReadSkill() missing skill content")
	}
	if !strings.HasPrefix(content, "[EXTERNAL SKILL CONTENT") {
		t.Errorf("ReadSkill() missing security prefix")
	}
	if !strings.HasSuffix(content, "[END EXTERNAL SKILL CONTENT]") {
		t.Errorf("ReadSkill() missing security suffix")
	}
}

func TestManagerReadSkillNotFound(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)

	mgr := NewManager(ManagerConfig{
		Memory:    store,
		SkillsDir: t.TempDir(),
	})

	_, err := mgr.ReadSkill("nonexistent-id")
	if err == nil {
		t.Fatal("ReadSkill() expected error for nonexistent node, got nil")
	}
}

func TestBumpPatch(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"1.0.0", "1.0.1"},
		{"1.2.3", "1.2.4"},
		{"0.0.9", "0.0.10"},
		{"", "1.0.0"},
		{"bad", "1.0.0"},
		{"1.0", "1.0.0"},
	}
	for _, tt := range tests {
		got := bumpPatch(tt.in)
		if got != tt.want {
			t.Errorf("bumpPatch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestUpdateSkillBumpsVersion(t *testing.T) {
	db := testDB(t)
	store := memory.NewStore(db)
	contentDir := t.TempDir()
	contentMgr := memory.NewContentManager(contentDir)
	skillsDir := t.TempDir()
	eventBus := bus.New()
	defer eventBus.Close()

	meta := SkillMeta{
		Slug:        "ver-skill",
		DisplayName: "Version Skill",
		Summary:     "Tests version bumping",
		Version:     "1.0.0",
	}
	zipMeta := SkillZipMeta{Slug: "ver-skill", Version: "1.0.0"}

	srv := testV1Server(t, meta, "# Original\n", zipMeta)

	ch := NewClawHub(srv.URL, nil)
	mgr := NewManager(ManagerConfig{
		ClawHub:   ch,
		Memory:    store,
		Content:   contentMgr,
		EventBus:  eventBus,
		SkillsDir: skillsDir,
	})

	nodeID, err := mgr.Install(context.Background(), meta)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	node, err := store.GetNode(nodeID)
	if err != nil {
		t.Fatalf("GetNode() error: %v", err)
	}
	if node.Version != "1.0.0" {
		t.Fatalf("initial version = %q, want %q", node.Version, "1.0.0")
	}

	updated, err := mgr.UpdateSkill(nodeID, "New Title", "", "# Updated\n")
	if err != nil {
		t.Fatalf("UpdateSkill() error: %v", err)
	}
	if updated.Version != "1.0.1" {
		t.Errorf("after update version = %q, want %q", updated.Version, "1.0.1")
	}

	// Second update should bump again.
	updated2, err := mgr.UpdateSkill(nodeID, "", "new summary", "")
	if err != nil {
		t.Fatalf("UpdateSkill() second error: %v", err)
	}
	if updated2.Version != "1.0.2" {
		t.Errorf("after second update version = %q, want %q", updated2.Version, "1.0.2")
	}
}
