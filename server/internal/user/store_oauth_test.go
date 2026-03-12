package user

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

func setupOAuthTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestCreateSocial(t *testing.T) {
	s := setupOAuthTestStore(t)

	u, err := s.CreateSocial("social@example.com", "Social User", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Email != "social@example.com" {
		t.Errorf("email = %q, want %q", u.Email, "social@example.com")
	}
	if u.Name != "Social User" {
		t.Errorf("name = %q, want %q", u.Name, "Social User")
	}
	if u.Role != RoleUser {
		t.Errorf("role = %q, want %q", u.Role, RoleUser)
	}
	if u.PasswordHash != "" {
		t.Errorf("expected empty password_hash, got %q", u.PasswordHash)
	}
}

func TestLinkAndGetByOAuthLink(t *testing.T) {
	s := setupOAuthTestStore(t)

	u, err := s.CreateSocial("gh@example.com", "GitHub User", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	if err := s.LinkOAuth(u.ID, "github", "gh-subject-123", "gh@example.com"); err != nil {
		t.Fatalf("LinkOAuth() error: %v", err)
	}

	got, err := s.GetByOAuthLink("github", "gh-subject-123")
	if err != nil {
		t.Fatalf("GetByOAuthLink() error: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("ID = %q, want %q", got.ID, u.ID)
	}
	if got.Email != "gh@example.com" {
		t.Errorf("email = %q, want %q", got.Email, "gh@example.com")
	}
}

func TestGetByOAuthLink_NotFound(t *testing.T) {
	s := setupOAuthTestStore(t)

	_, err := s.GetByOAuthLink("github", "nonexistent-subject")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListOAuthLinks(t *testing.T) {
	s := setupOAuthTestStore(t)

	u, err := s.CreateSocial("multilink@example.com", "Multi Link User", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	if err := s.LinkOAuth(u.ID, "github", "gh-subj", "gh@example.com"); err != nil {
		t.Fatalf("LinkOAuth(github) error: %v", err)
	}
	if err := s.LinkOAuth(u.ID, "google", "gg-subj", "gg@example.com"); err != nil {
		t.Fatalf("LinkOAuth(google) error: %v", err)
	}

	links, err := s.ListOAuthLinks(u.ID)
	if err != nil {
		t.Fatalf("ListOAuthLinks() error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
	for _, l := range links {
		if l.UserID != u.ID {
			t.Errorf("link user_id = %q, want %q", l.UserID, u.ID)
		}
		if l.ID == "" {
			t.Error("expected non-empty link ID")
		}
	}
}

func TestUnlinkOAuth(t *testing.T) {
	s := setupOAuthTestStore(t)

	u, err := s.CreateSocial("unlinkme@example.com", "Unlink Me", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	if err := s.LinkOAuth(u.ID, "github", "gh-subj-unlink", "unlink@example.com"); err != nil {
		t.Fatalf("LinkOAuth() error: %v", err)
	}

	if err := s.UnlinkOAuth(u.ID, "github"); err != nil {
		t.Fatalf("UnlinkOAuth() error: %v", err)
	}

	_, err = s.GetByOAuthLink("github", "gh-subj-unlink")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after unlink, got %v", err)
	}
}

func TestUnlinkOAuth_NotFound(t *testing.T) {
	s := setupOAuthTestStore(t)

	u, err := s.CreateSocial("nolinkuser@example.com", "No Link User", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	err = s.UnlinkOAuth(u.ID, "github")
	if !errors.Is(err, ErrLinkNotFound) {
		t.Errorf("expected ErrLinkNotFound, got %v", err)
	}
}

func TestHasPassword(t *testing.T) {
	s := setupOAuthTestStore(t)

	// Password user should return true.
	pw, err := s.Create(CreateUserInput{
		Email:    "pwuser@example.com",
		Password: "secret",
		Role:     RoleUser,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	has, err := s.HasPassword(pw.ID)
	if err != nil {
		t.Fatalf("HasPassword(pw user) error: %v", err)
	}
	if !has {
		t.Error("expected HasPassword = true for password user")
	}

	// Social user should return false.
	social, err := s.CreateSocial("socialonly@example.com", "Social Only", RoleUser)
	if err != nil {
		t.Fatalf("CreateSocial() error: %v", err)
	}

	has, err = s.HasPassword(social.ID)
	if err != nil {
		t.Fatalf("HasPassword(social user) error: %v", err)
	}
	if has {
		t.Error("expected HasPassword = false for social user")
	}
}
