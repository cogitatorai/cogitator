package push

import (
	"github.com/cogitatorai/cogitator/server/internal/database"
)

type Token struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	Token     string `json:"token"`
	Platform  string `json:"platform"`
	CreatedAt string `json:"created_at"`
}

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// Upsert registers or updates a push token. If the token already exists
// (for a different user), it reassigns it.
func (s *Store) Upsert(userID, token, platform string) error {
	_, err := s.db.Exec(`INSERT INTO push_tokens (user_id, token, platform, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(token) DO UPDATE SET user_id = excluded.user_id, platform = excluded.platform`,
		userID, token, platform)
	return err
}

// ListByUser returns all push tokens for a given user.
func (s *Store) ListByUser(userID string) ([]Token, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, token, platform, created_at FROM push_tokens WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Token, &t.Platform, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Token{}
	}
	return out, rows.Err()
}

// ListAll returns all registered push tokens across all users.
func (s *Store) ListAll() ([]Token, error) {
	rows, err := s.db.Query(`SELECT id, user_id, token, platform, created_at FROM push_tokens`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Token, &t.Platform, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []Token{}
	}
	return out, rows.Err()
}

// DeleteByToken removes a specific push token.
func (s *Store) DeleteByToken(token string) error {
	_, err := s.db.Exec(`DELETE FROM push_tokens WHERE token = ?`, token)
	return err
}

// DeleteByUser removes all push tokens for a user (used on logout).
func (s *Store) DeleteByUser(userID string) error {
	_, err := s.db.Exec(`DELETE FROM push_tokens WHERE user_id = ?`, userID)
	return err
}
