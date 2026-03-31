package user

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrInvalidCreds  = errors.New("invalid credentials")
	ErrCodeRedeemed  = errors.New("invite code already redeemed")
	ErrCodeExpired   = errors.New("invite code expired")
	ErrCodeNotFound  = errors.New("invite code not found")
	ErrTokenNotFound = errors.New("refresh token not found")
	ErrTokenExpired  = errors.New("refresh token expired")
	ErrDuplicateUser = errors.New("email already exists")
	ErrLinkNotFound  = errors.New("oauth link not found")
)

// Store provides CRUD operations for users, invite codes, and refresh tokens.
type Store struct {
	db *database.DB
}

// NewStore creates a new user store backed by the given database.
func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// Create hashes the password and inserts a new user.
func (s *Store) Create(input CreateUserInput) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	u := &User{
		ID:           uuid.New().String(),
		Email:        input.Email,
		Name:         input.Name,
		PasswordHash: string(hash),
		Role:         input.Role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err = s.db.Exec(
		`INSERT INTO users (id, email, name, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, u.PasswordHash, string(u.Role), u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateUser
		}
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	return u, nil
}

// Get retrieves a user by ID.
func (s *Store) Get(id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, email, name, password_hash, role, profile_overrides, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.ProfileOverrides, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}
	return u, nil
}

// GetByEmail retrieves a user by email (case-insensitive).
func (s *Store) GetByEmail(email string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, email, name, password_hash, role, profile_overrides, created_at, updated_at
		 FROM users WHERE email = ? COLLATE NOCASE`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.ProfileOverrides, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by email: %w", err)
	}
	return u, nil
}

// List returns all users ordered by creation time.
func (s *Store) List() ([]User, error) {
	rows, err := s.db.Query(
		`SELECT id, email, name, password_hash, role, profile_overrides, created_at, updated_at
		 FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.ProfileOverrides, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Count returns the total number of users.
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// UpdateRole changes a user's role.
func (s *Store) UpdateRole(id string, role Role) error {
	res, err := s.db.Exec(`UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, string(role), time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("updating role: %w", err)
	}
	return checkAffected(res)
}

// UpdateProfile sets a user's profile overrides.
func (s *Store) UpdateProfile(id string, overrides string) error {
	res, err := s.db.Exec(`UPDATE users SET profile_overrides = ?, updated_at = ? WHERE id = ?`, overrides, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("updating profile: %w", err)
	}
	return checkAffected(res)
}

// UpdateUser updates a user's name and optionally their password hash.
func (s *Store) UpdateUser(id string, name string, passwordHash *string) error {
	now := time.Now().UTC()
	var res sql.Result
	var err error
	if passwordHash != nil {
		res, err = s.db.Exec(
			`UPDATE users SET name = ?, password_hash = ?, updated_at = ? WHERE id = ?`,
			name, *passwordHash, now, id,
		)
	} else {
		res, err = s.db.Exec(
			`UPDATE users SET name = ?, updated_at = ? WHERE id = ?`,
			name, now, id,
		)
	}
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	return checkAffected(res)
}

// UpdateEmail changes a user's email. Returns ErrDuplicateUser if the
// new email is already taken.
func (s *Store) UpdateEmail(id, newEmail string) error {
	res, err := s.db.Exec(
		`UPDATE users SET email = ?, updated_at = ? WHERE id = ?`,
		newEmail, time.Now().UTC(), id,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDuplicateUser
		}
		return fmt.Errorf("updating email: %w", err)
	}
	return checkAffected(res)
}

// Delete removes a user by ID.
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return checkAffected(res)
}

// Authenticate verifies credentials and returns the user. Returns a generic
// error for both "user not found" and "wrong password" to avoid leaking info.
func (s *Store) Authenticate(email, password string) (*User, error) {
	u, err := s.GetByEmail(email)
	if err != nil {
		return nil, ErrInvalidCreds
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCreds
	}
	return u, nil
}

// CreateInviteCode generates an invite code in "XXXX-XXXX-XXXX" format.
func (s *Store) CreateInviteCode(input CreateInviteInput) (*InviteCode, error) {
	code := generateInviteCode()
	now := time.Now().UTC()

	ic := &InviteCode{
		Code:      code,
		CreatedBy: input.CreatedBy,
		Role:      input.Role,
		ExpiresAt: input.ExpiresAt,
		CreatedAt: now,
	}

	_, err := s.db.Exec(
		`INSERT INTO invite_codes (code, created_by, role, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		ic.Code, ic.CreatedBy, string(ic.Role), ic.ExpiresAt, ic.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting invite code: %w", err)
	}

	return ic, nil
}

// RedeemInviteCode marks an invite code as used. It validates the code has not
// already been redeemed and has not expired.
func (s *Store) RedeemInviteCode(code, userID string) (*InviteCode, error) {
	ic := &InviteCode{}
	err := s.db.QueryRow(
		`SELECT code, created_by, role, redeemed_by, expires_at, created_at
		 FROM invite_codes WHERE code = ?`, code,
	).Scan(&ic.Code, &ic.CreatedBy, &ic.Role, &ic.RedeemedBy, &ic.ExpiresAt, &ic.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying invite code: %w", err)
	}

	if ic.RedeemedBy != nil {
		return nil, ErrCodeRedeemed
	}
	if ic.ExpiresAt != nil && ic.ExpiresAt.Before(time.Now()) {
		return nil, ErrCodeExpired
	}

	_, err = s.db.Exec(
		`UPDATE invite_codes SET redeemed_by = ? WHERE code = ?`,
		userID, code,
	)
	if err != nil {
		return nil, fmt.Errorf("redeeming invite code: %w", err)
	}

	ic.RedeemedBy = &userID
	return ic, nil
}

// ListInviteCodes returns all invite codes, newest first.
func (s *Store) ListInviteCodes() ([]InviteCode, error) {
	rows, err := s.db.Query(
		`SELECT code, created_by, role, redeemed_by, expires_at, created_at
		 FROM invite_codes ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing invite codes: %w", err)
	}
	defer rows.Close()

	var codes []InviteCode
	for rows.Next() {
		var ic InviteCode
		if err := rows.Scan(&ic.Code, &ic.CreatedBy, &ic.Role, &ic.RedeemedBy, &ic.ExpiresAt, &ic.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning invite code: %w", err)
		}
		codes = append(codes, ic)
	}
	return codes, rows.Err()
}

// DeleteInviteCode removes an invite code.
func (s *Store) DeleteInviteCode(code string) error {
	_, err := s.db.Exec(`DELETE FROM invite_codes WHERE code = ?`, code)
	return err
}

// StoreRefreshToken persists a hashed refresh token.
func (s *Store) StoreRefreshToken(tokenHash, userID string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO refresh_tokens (token_hash, user_id, expires_at, created_at)
		 VALUES (?, ?, ?, ?)`,
		tokenHash, userID, expiresAt.UTC(), time.Now().UTC(),
	)
	return err
}

// ValidateRefreshToken checks that the token exists and is not expired.
// Returns the associated user ID.
func (s *Store) ValidateRefreshToken(tokenHash string) (string, error) {
	var userID string
	var expiresAt time.Time
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM refresh_tokens WHERE token_hash = ?`, tokenHash,
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTokenNotFound
	}
	if err != nil {
		return "", fmt.Errorf("querying refresh token: %w", err)
	}
	if expiresAt.Before(time.Now()) {
		return "", ErrTokenExpired
	}
	return userID, nil
}

// RevokeRefreshToken deletes a single refresh token.
func (s *Store) RevokeRefreshToken(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE token_hash = ?`, tokenHash)
	return err
}

// RevokeAllRefreshTokens deletes all refresh tokens for a given user.
func (s *Store) RevokeAllRefreshTokens(userID string) error {
	_, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE user_id = ?`, userID)
	return err
}

// CleanupExpiredTokens removes all expired refresh tokens and returns
// the number deleted.
func (s *Store) CleanupExpiredTokens() (int, error) {
	res, err := s.db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// generateInviteCode produces a code in "XXXX-XXXX-XXXX" format using
// cryptographically random uppercase hex characters.
func generateInviteCode() string {
	b := make([]byte, 6) // 6 random bytes = 12 hex chars
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	hex := fmt.Sprintf("%X", b) // 12 uppercase hex chars
	return hex[0:4] + "-" + hex[4:8] + "-" + hex[8:12]
}

// CreateSocial inserts a new user with an empty password hash for social-only accounts.
func (s *Store) CreateSocial(email, name string, role Role) (*User, error) {
	now := time.Now().UTC()
	u := &User{
		ID:        uuid.New().String(),
		Email:     email,
		Name:      name,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := s.db.Exec(
		`INSERT INTO users (id, email, name, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, "", string(u.Role), u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateUser
		}
		return nil, fmt.Errorf("inserting social user: %w", err)
	}

	return u, nil
}

// LinkOAuth inserts a new OAuth link record associating a provider identity with a user.
func (s *Store) LinkOAuth(userID, provider, subject, email string) error {
	_, err := s.db.Exec(
		`INSERT INTO user_oauth_links (id, user_id, provider, subject, email, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), userID, provider, subject, email, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting oauth link: %w", err)
	}
	return nil
}

// GetByOAuthLink returns the user associated with the given provider and subject.
// Returns ErrNotFound if no matching link exists.
func (s *Store) GetByOAuthLink(provider, subject string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT u.id, u.email, u.name, u.password_hash, u.role, u.profile_overrides, u.created_at, u.updated_at
		 FROM users u
		 JOIN user_oauth_links l ON l.user_id = u.id
		 WHERE l.provider = ? AND l.subject = ?`,
		provider, subject,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.ProfileOverrides, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by oauth link: %w", err)
	}
	return u, nil
}

// ListOAuthLinks returns all OAuth links for the given user, ordered by creation time.
func (s *Store) ListOAuthLinks(userID string) ([]OAuthLink, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, provider, subject, email, created_at
		 FROM user_oauth_links WHERE user_id = ? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing oauth links: %w", err)
	}
	defer rows.Close()

	var links []OAuthLink
	for rows.Next() {
		var l OAuthLink
		if err := rows.Scan(&l.ID, &l.UserID, &l.Provider, &l.Subject, &l.Email, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning oauth link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// UnlinkOAuth removes the OAuth link for the given user and provider.
// Returns ErrLinkNotFound if no such link exists.
func (s *Store) UnlinkOAuth(userID, provider string) error {
	res, err := s.db.Exec(
		`DELETE FROM user_oauth_links WHERE user_id = ? AND provider = ?`,
		userID, provider,
	)
	if err != nil {
		return fmt.Errorf("deleting oauth link: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLinkNotFound
	}
	return nil
}

// HasPassword reports whether the user has a non-empty password hash set.
func (s *Store) HasPassword(userID string) (bool, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("querying password hash: %w", err)
	}
	return hash != "", nil
}

// checkAffected returns ErrNotFound if no rows were affected by an update/delete.
func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
