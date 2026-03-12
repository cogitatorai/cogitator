package user

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestCreateUser(t *testing.T) {
	s := setupTestDB(t)

	u, err := s.Create(CreateUserInput{
		Email:    "alice",
		Name: "Alice",
		Password:    "secret123",
		Role:        RoleAdmin,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Email != "alice" {
		t.Errorf("email = %q, want %q", u.Email, "alice")
	}
	if u.Name != "Alice" {
		t.Errorf("name = %q, want %q", u.Name, "Alice")
	}
	if u.Role != RoleAdmin {
		t.Errorf("role = %q, want %q", u.Role, RoleAdmin)
	}
	if u.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
	if u.PasswordHash == "secret123" {
		t.Error("password hash should not be the raw password")
	}
	if u.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	s := setupTestDB(t)

	_, err := s.Create(CreateUserInput{
		Email: "bob",
		Password: "pass1",
		Role:     RoleUser,
	})
	if err != nil {
		t.Fatalf("first Create() error: %v", err)
	}

	_, err = s.Create(CreateUserInput{
		Email: "bob",
		Password: "pass2",
		Role:     RoleUser,
	})
	if err == nil {
		t.Fatal("expected error for duplicate email, got nil")
	}
}

func TestGetUser(t *testing.T) {
	s := setupTestDB(t)

	created, _ := s.Create(CreateUserInput{
		Email: "charlie",
		Password: "pass",
		Role:     RoleUser,
	})

	got, err := s.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Email != "charlie" {
		t.Errorf("email = %q, want %q", got.Email, "charlie")
	}
}

func TestGetUserByEmail(t *testing.T) {
	s := setupTestDB(t)

	_, _ = s.Create(CreateUserInput{
		Email:    "Diana@Example.com",
		Password: "pass",
		Role:     RoleUser,
	})

	// Should be case-insensitive.
	got, err := s.GetByEmail("diana@example.com")
	if err != nil {
		t.Fatalf("GetByEmail() error: %v", err)
	}
	if got.Email != "Diana@Example.com" {
		t.Errorf("email = %q, want %q", got.Email, "Diana@Example.com")
	}

	// Uppercase variant.
	got2, err := s.GetByEmail("DIANA@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetByEmail(upper) error: %v", err)
	}
	if got2.ID != got.ID {
		t.Errorf("expected same user for case-insensitive lookup")
	}
}

func TestListUsers(t *testing.T) {
	s := setupTestDB(t)

	for _, name := range []string{"u1", "u2", "u3"} {
		_, err := s.Create(CreateUserInput{
			Email: name,
			Password: "pass",
			Role:     RoleUser,
		})
		if err != nil {
			t.Fatalf("Create(%s) error: %v", name, err)
		}
	}

	users, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("got %d users, want 3", len(users))
	}
}

func TestUpdateUserRole(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{
		Email: "eve",
		Password: "pass",
		Role:     RoleUser,
	})

	if err := s.UpdateRole(u.ID, RoleModerator); err != nil {
		t.Fatalf("UpdateRole() error: %v", err)
	}

	got, _ := s.Get(u.ID)
	if got.Role != RoleModerator {
		t.Errorf("role = %q, want %q", got.Role, RoleModerator)
	}
}

func TestUpdateProfile(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{
		Email: "frank",
		Password: "pass",
		Role:     RoleUser,
	})

	overrides := `{"model":"gpt-4"}`
	if err := s.UpdateProfile(u.ID, overrides); err != nil {
		t.Fatalf("UpdateProfile() error: %v", err)
	}

	got, _ := s.Get(u.ID)
	if got.ProfileOverrides != overrides {
		t.Errorf("profile_overrides = %q, want %q", got.ProfileOverrides, overrides)
	}
}

func TestDeleteUser(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{
		Email: "gina",
		Password: "pass",
		Role:     RoleUser,
	})

	if err := s.Delete(u.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err := s.Get(u.ID)
	if err == nil {
		t.Error("expected error after deleting user, got nil")
	}
}

func TestAuthenticate(t *testing.T) {
	s := setupTestDB(t)

	_, _ = s.Create(CreateUserInput{
		Email: "hank",
		Password: "correctpassword",
		Role:     RoleUser,
	})

	// Valid credentials.
	u, err := s.Authenticate("hank", "correctpassword")
	if err != nil {
		t.Fatalf("Authenticate(valid) error: %v", err)
	}
	if u.Email != "hank" {
		t.Errorf("email = %q, want %q", u.Email, "hank")
	}

	// Invalid password.
	_, err = s.Authenticate("hank", "wrongpassword")
	if err == nil {
		t.Error("expected error for invalid password, got nil")
	}

	// Non-existent user.
	_, err = s.Authenticate("nobody", "pass")
	if err == nil {
		t.Error("expected error for non-existent user, got nil")
	}
}

func TestUserCount(t *testing.T) {
	s := setupTestDB(t)

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count() error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	_, _ = s.Create(CreateUserInput{Email: "c1", Password: "p", Role: RoleUser})
	_, _ = s.Create(CreateUserInput{Email: "c2", Password: "p", Role: RoleUser})

	count, err = s.Count()
	if err != nil {
		t.Fatalf("Count() error: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCreateInviteCode(t *testing.T) {
	s := setupTestDB(t)

	// Need a user as the creator.
	admin, _ := s.Create(CreateUserInput{
		Email: "admin",
		Password: "pass",
		Role:     RoleAdmin,
	})

	code, err := s.CreateInviteCode(CreateInviteInput{
		CreatedBy: admin.ID,
		Role:      RoleUser,
	})
	if err != nil {
		t.Fatalf("CreateInviteCode() error: %v", err)
	}

	// Format check: "XXXX-XXXX-XXXX" (uppercase hex, 4-4-4).
	parts := strings.Split(code.Code, "-")
	if len(parts) != 3 {
		t.Fatalf("code format: got %q, want 3 parts separated by dashes", code.Code)
	}
	for i, part := range parts {
		if len(part) != 4 {
			t.Errorf("part %d length = %d, want 4", i, len(part))
		}
		if part != strings.ToUpper(part) {
			t.Errorf("part %d = %q, want uppercase", i, part)
		}
	}

	if code.CreatedBy != admin.ID {
		t.Errorf("created_by = %q, want %q", code.CreatedBy, admin.ID)
	}
	if code.Role != RoleUser {
		t.Errorf("role = %q, want %q", code.Role, RoleUser)
	}
	if code.RedeemedBy != nil {
		t.Error("expected redeemed_by to be nil")
	}
}

func TestRedeemInviteCode(t *testing.T) {
	s := setupTestDB(t)

	admin, _ := s.Create(CreateUserInput{
		Email: "admin2",
		Password: "pass",
		Role:     RoleAdmin,
	})

	invite, _ := s.CreateInviteCode(CreateInviteInput{
		CreatedBy: admin.ID,
		Role:      RoleUser,
	})

	newUser, _ := s.Create(CreateUserInput{
		Email: "newbie",
		Password: "pass",
		Role:     RoleUser,
	})

	redeemed, err := s.RedeemInviteCode(invite.Code, newUser.ID)
	if err != nil {
		t.Fatalf("RedeemInviteCode() error: %v", err)
	}
	if redeemed.RedeemedBy == nil || *redeemed.RedeemedBy != newUser.ID {
		t.Errorf("redeemed_by = %v, want %q", redeemed.RedeemedBy, newUser.ID)
	}
}

func TestRedeemInviteCode_AlreadyRedeemed(t *testing.T) {
	s := setupTestDB(t)

	admin, _ := s.Create(CreateUserInput{Email: "a1", Password: "p", Role: RoleAdmin})
	invite, _ := s.CreateInviteCode(CreateInviteInput{CreatedBy: admin.ID, Role: RoleUser})
	u1, _ := s.Create(CreateUserInput{Email: "r1", Password: "p", Role: RoleUser})
	u2, _ := s.Create(CreateUserInput{Email: "r2", Password: "p", Role: RoleUser})

	_, _ = s.RedeemInviteCode(invite.Code, u1.ID)

	_, err := s.RedeemInviteCode(invite.Code, u2.ID)
	if err == nil {
		t.Fatal("expected error redeeming already-redeemed code, got nil")
	}
}

func TestRedeemInviteCode_Expired(t *testing.T) {
	s := setupTestDB(t)

	admin, _ := s.Create(CreateUserInput{Email: "a2", Password: "p", Role: RoleAdmin})
	past := time.Now().Add(-1 * time.Hour)
	invite, _ := s.CreateInviteCode(CreateInviteInput{
		CreatedBy: admin.ID,
		Role:      RoleUser,
		ExpiresAt: &past,
	})

	u, _ := s.Create(CreateUserInput{Email: "r3", Password: "p", Role: RoleUser})

	_, err := s.RedeemInviteCode(invite.Code, u.ID)
	if err == nil {
		t.Fatal("expected error redeeming expired code, got nil")
	}
}

func TestListInviteCodes(t *testing.T) {
	s := setupTestDB(t)

	admin, _ := s.Create(CreateUserInput{Email: "la", Password: "p", Role: RoleAdmin})

	for i := 0; i < 3; i++ {
		_, err := s.CreateInviteCode(CreateInviteInput{CreatedBy: admin.ID, Role: RoleUser})
		if err != nil {
			t.Fatalf("CreateInviteCode(%d) error: %v", i, err)
		}
	}

	codes, err := s.ListInviteCodes()
	if err != nil {
		t.Fatalf("ListInviteCodes() error: %v", err)
	}
	if len(codes) != 3 {
		t.Errorf("got %d codes, want 3", len(codes))
	}
}

func TestRefreshToken_StoreAndValidate(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{Email: "rt1", Password: "p", Role: RoleUser})
	hash := "somehash123"
	exp := time.Now().Add(24 * time.Hour)

	if err := s.StoreRefreshToken(hash, u.ID, exp); err != nil {
		t.Fatalf("StoreRefreshToken() error: %v", err)
	}

	userID, err := s.ValidateRefreshToken(hash)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error: %v", err)
	}
	if userID != u.ID {
		t.Errorf("userID = %q, want %q", userID, u.ID)
	}
}

func TestRefreshToken_Expired(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{Email: "rt2", Password: "p", Role: RoleUser})
	hash := "expiredhash"
	exp := time.Now().Add(-1 * time.Hour) // already expired

	_ = s.StoreRefreshToken(hash, u.ID, exp)

	_, err := s.ValidateRefreshToken(hash)
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
}

func TestRevokeAllRefreshTokens(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{Email: "rt3", Password: "p", Role: RoleUser})
	exp := time.Now().Add(24 * time.Hour)

	_ = s.StoreRefreshToken("h1", u.ID, exp)
	_ = s.StoreRefreshToken("h2", u.ID, exp)
	_ = s.StoreRefreshToken("h3", u.ID, exp)

	if err := s.RevokeAllRefreshTokens(u.ID); err != nil {
		t.Fatalf("RevokeAllRefreshTokens() error: %v", err)
	}

	// All tokens should now be invalid.
	for _, h := range []string{"h1", "h2", "h3"} {
		_, err := s.ValidateRefreshToken(h)
		if err == nil {
			t.Errorf("expected error for revoked token %q, got nil", h)
		}
	}
}

func TestUpdateUser(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{
		Email:    "upd1",
		Name: "Original",
		Password:    "pass",
		Role:        RoleUser,
	})

	// Update display name only (no password change).
	if err := s.UpdateUser(u.ID, "Updated Name", nil); err != nil {
		t.Fatalf("UpdateUser() error: %v", err)
	}

	got, _ := s.Get(u.ID)
	if got.Name != "Updated Name" {
		t.Errorf("name = %q, want %q", got.Name, "Updated Name")
	}

	// Update with new password.
	newHash := "$2a$10$fakehashfortest"
	if err := s.UpdateUser(u.ID, "Updated Again", &newHash); err != nil {
		t.Fatalf("UpdateUser(with password) error: %v", err)
	}

	got2, _ := s.Get(u.ID)
	if got2.Name != "Updated Again" {
		t.Errorf("name = %q, want %q", got2.Name, "Updated Again")
	}
	if got2.PasswordHash != newHash {
		t.Errorf("password_hash = %q, want %q", got2.PasswordHash, newHash)
	}
}

func TestDeleteInviteCode(t *testing.T) {
	s := setupTestDB(t)

	admin, _ := s.Create(CreateUserInput{Email: "del_admin", Password: "p", Role: RoleAdmin})
	invite, _ := s.CreateInviteCode(CreateInviteInput{CreatedBy: admin.ID, Role: RoleUser})

	if err := s.DeleteInviteCode(invite.Code); err != nil {
		t.Fatalf("DeleteInviteCode() error: %v", err)
	}

	codes, _ := s.ListInviteCodes()
	if len(codes) != 0 {
		t.Errorf("got %d codes after delete, want 0", len(codes))
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{Email: "rev1", Password: "p", Role: RoleUser})
	exp := time.Now().Add(24 * time.Hour)
	hash := "revokeme"

	_ = s.StoreRefreshToken(hash, u.ID, exp)

	if err := s.RevokeRefreshToken(hash); err != nil {
		t.Fatalf("RevokeRefreshToken() error: %v", err)
	}

	_, err := s.ValidateRefreshToken(hash)
	if err == nil {
		t.Error("expected error for revoked token, got nil")
	}
}

func TestCleanupExpiredTokens(t *testing.T) {
	s := setupTestDB(t)

	u, _ := s.Create(CreateUserInput{Email: "clean1", Password: "p", Role: RoleUser})

	// Two expired, one valid.
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(24 * time.Hour)
	_ = s.StoreRefreshToken("exp1", u.ID, past)
	_ = s.StoreRefreshToken("exp2", u.ID, past)
	_ = s.StoreRefreshToken("valid1", u.ID, future)

	count, err := s.CleanupExpiredTokens()
	if err != nil {
		t.Fatalf("CleanupExpiredTokens() error: %v", err)
	}
	if count != 2 {
		t.Errorf("cleaned up %d tokens, want 2", count)
	}

	// The valid one should still work.
	_, err = s.ValidateRefreshToken("valid1")
	if err != nil {
		t.Errorf("valid token should still work: %v", err)
	}
}
