package session

import (
	"database/sql"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

type Message struct {
	ID         int64     `json:"id"`
	SessionKey string    `json:"session_key"`
	UserID     string    `json:"user_id,omitempty"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	ToolCalls  string    `json:"tool_calls,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	ToolsUsed  string    `json:"tools_used,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Session struct {
	Key        string    `json:"key"`
	Channel    string    `json:"channel"`
	ChatID     string    `json:"chat_id"`
	UserID     string    `json:"user_id"`
	Private    bool      `json:"private"`
	Summary    string    `json:"summary,omitempty"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastActive time.Time `json:"last_active"`
}

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// TasksOutputKey returns the per-user session key for the pinned Tasks
// messages list. Each user gets their own session so task results and
// notifications are scoped correctly.
func TasksOutputKey(userID string) string {
	if userID == "" {
		return "tasks:output"
	}
	return "tasks:output:" + userID
}

const sessionColumns = `key, channel, chat_id, user_id, private, summary, is_active, created_at, updated_at, last_active`

func scanSession(scanner interface{ Scan(...any) error }) (*Session, error) {
	var sess Session
	var summary sql.NullString
	var userID sql.NullString
	err := scanner.Scan(
		&sess.Key, &sess.Channel, &sess.ChatID, &userID, &sess.Private,
		&summary, &sess.IsActive,
		&sess.CreatedAt, &sess.UpdatedAt, &sess.LastActive)
	if err != nil {
		return nil, err
	}
	sess.UserID = userID.String
	sess.Summary = summary.String
	return &sess, nil
}

func (s *Store) GetOrCreate(key, channel, chatID, userID string, private bool) (*Session, error) {
	sess, err := scanSession(s.db.QueryRow(
		`SELECT `+sessionColumns+` FROM sessions WHERE key = ?`, key))

	if err == sql.ErrNoRows {
		now := time.Now()
		var uid any
		if userID != "" {
			uid = userID
		}
		_, err = s.db.Exec(`INSERT INTO sessions (key, channel, chat_id, user_id, private, created_at, updated_at, last_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, key, channel, chatID, uid, private, now, now, now)
		if err != nil {
			return nil, err
		}
		return &Session{
			Key: key, Channel: channel, ChatID: chatID,
			UserID: userID, Private: private,
			CreatedAt: now, UpdatedAt: now, LastActive: now,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	// Claim orphaned sessions: if the existing session has no owner and a
	// logged-in user is accessing it, bind it to that user so it appears in
	// their session list. This covers sessions created before multi-user was
	// enabled.
	if sess.UserID == "" && userID != "" {
		s.db.Exec(`UPDATE sessions SET user_id = ? WHERE key = ? AND user_id IS NULL`, userID, key)
		sess.UserID = userID
	}
	return sess, nil
}

func (s *Store) Get(key, userID string) (*Session, error) {
	if userID == "" {
		return scanSession(s.db.QueryRow(
			`SELECT `+sessionColumns+` FROM sessions WHERE key = ? AND user_id IS NULL`, key))
	}
	return scanSession(s.db.QueryRow(
		`SELECT `+sessionColumns+` FROM sessions WHERE key = ? AND user_id = ?`, key, userID))
}

// GetByKey looks up a session by key without any user ownership filter.
// This is intended for background workers that need session metadata (e.g.
// the Private flag) but do not operate on behalf of a specific user.
func (s *Store) GetByKey(key string) (*Session, error) {
	return scanSession(s.db.QueryRow(
		`SELECT `+sessionColumns+` FROM sessions WHERE key = ?`, key))
}

func (s *Store) List(userID string) ([]Session, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = s.db.Query(`SELECT `+sessionColumns+`
			FROM sessions WHERE user_id IS NULL AND channel NOT IN ('task', 'tasks') ORDER BY last_active DESC`)
	} else {
		rows, err = s.db.Query(`SELECT `+sessionColumns+`
			FROM sessions WHERE user_id = ? AND channel NOT IN ('task', 'tasks') ORDER BY last_active DESC`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Delete(key, userID string) error {
	if userID == "" {
		_, err := s.db.Exec("DELETE FROM sessions WHERE key = ? AND user_id IS NULL", key)
		return err
	}
	_, err := s.db.Exec("DELETE FROM sessions WHERE key = ? AND user_id = ?", key, userID)
	return err
}

func (s *Store) AddMessage(sessionKey string, msg Message) (int64, error) {
	now := time.Now()
	var uid any
	if msg.UserID != "" {
		uid = msg.UserID
	}
	result, err := s.db.Exec(`INSERT INTO messages (session_key, user_id, role, content, tool_calls, tool_call_id, tools_used, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionKey, uid, msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, msg.ToolsUsed, now)
	if err != nil {
		return 0, err
	}

	s.db.Exec("UPDATE sessions SET updated_at = ?, last_active = ? WHERE key = ?", now, now, sessionKey)

	return result.LastInsertId()
}

func (s *Store) GetMessages(sessionKey string, limit int) ([]Message, error) {
	query := `SELECT id, session_key, role, content, tool_calls, tool_call_id, tools_used, created_at
		FROM messages WHERE session_key = ? ORDER BY id ASC`
	var args []any
	args = append(args, sessionKey)

	if limit > 0 {
		query = `SELECT * FROM (
			SELECT id, session_key, role, content, tool_calls, tool_call_id, tools_used, created_at
			FROM messages WHERE session_key = ? ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var toolCalls, toolCallID, toolsUsed sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content,
			&toolCalls, &toolCallID, &toolsUsed, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ToolCalls = toolCalls.String
		m.ToolCallID = toolCallID.String
		m.ToolsUsed = toolsUsed.String
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *Store) MessageCount(sessionKey string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_key = ?", sessionKey).Scan(&count)
	return count, err
}

func (s *Store) SetSummary(sessionKey, summary string) error {
	_, err := s.db.Exec("UPDATE sessions SET summary = ?, updated_at = ? WHERE key = ?",
		summary, time.Now(), sessionKey)
	return err
}

func (s *Store) TruncateMessages(sessionKey string, keepLast int) error {
	if keepLast <= 0 {
		_, err := s.db.Exec("DELETE FROM messages WHERE session_key = ?", sessionKey)
		return err
	}

	_, err := s.db.Exec(`DELETE FROM messages WHERE session_key = ? AND id NOT IN (
		SELECT id FROM messages WHERE session_key = ? ORDER BY id DESC LIMIT ?
	)`, sessionKey, sessionKey, keepLast)
	return err
}

// DeleteMessage removes a single message by ID.
func (s *Store) DeleteMessage(id int64) error {
	_, err := s.db.Exec("DELETE FROM messages WHERE id = ?", id)
	return err
}

// SetActiveSession marks a session as the active one for its channel+user.
// All other sessions on the same channel for the same user are deactivated.
func (s *Store) SetActiveSession(key, userID string) error {
	sess, err := s.Get(key, userID)
	if err != nil {
		return err
	}
	if userID == "" {
		_, err = s.db.Exec("UPDATE sessions SET is_active = 0 WHERE channel = ? AND user_id IS NULL", sess.Channel)
	} else {
		_, err = s.db.Exec("UPDATE sessions SET is_active = 0 WHERE channel = ? AND user_id = ?", sess.Channel, userID)
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE sessions SET is_active = 1 WHERE key = ?", key)
	return err
}

// GetActiveSessions returns the active session for each channel belonging to the
// given user. Task sessions are excluded.
func (s *Store) GetActiveSessions(userID string) ([]Session, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = s.db.Query(`SELECT `+sessionColumns+`
			FROM sessions WHERE is_active = 1 AND user_id IS NULL AND channel NOT IN ('task', 'tasks')`)
	} else {
		rows, err = s.db.Query(`SELECT `+sessionColumns+`
			FROM sessions WHERE is_active = 1 AND user_id = ? AND channel NOT IN ('task', 'tasks')`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *sess)
	}
	return sessions, rows.Err()
}
