package user

import "testing"

func TestBootstrap_CreatesAdmin(t *testing.T) {
	store := setupTestDB(t)

	u, err := store.Bootstrap("admin", "secret123")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.Email != "admin" {
		t.Errorf("email = %q, want %q", u.Email, "admin")
	}
	if u.Name != "Admin" {
		t.Errorf("name = %q, want %q", u.Name, "Admin")
	}
	if u.Role != RoleAdmin {
		t.Errorf("role = %q, want %q", u.Role, RoleAdmin)
	}

	// Verify the user was persisted.
	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	store := setupTestDB(t)

	// Seed an existing user.
	_, err := store.Create(CreateUserInput{
		Email:    "existing@test.com",
		Name:     "Existing User",
		Password: "password",
		Role:     RoleUser,
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	u, err := store.Bootstrap("admin", "secret123")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil (already bootstrapped), got user %q", u.Email)
	}

	// Verify no extra user was created.
	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestBootstrap_MissingCredentials(t *testing.T) {
	store := setupTestDB(t)

	cases := []struct {
		name     string
		email    string
		password string
	}{
		{"both empty", "", ""},
		{"empty email", "", "password"},
		{"empty password", "admin@test.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := store.Bootstrap(tc.email, tc.password)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if u != nil {
				t.Fatalf("expected nil user, got %+v", u)
			}
		})
	}
}
