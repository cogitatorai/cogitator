package notification

import (
	"database/sql"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/database"
)

type Notification struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id,omitempty"`
	TaskID    *int64    `json:"task_id,omitempty"`
	TaskName  string    `json:"task_name"`
	RunID     int64     `json:"run_id"`
	Trigger   string    `json:"trigger"`
	Status    string    `json:"status"`
	Content   string    `json:"content"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db *database.DB
}

func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(n *Notification) (int64, error) {
	var uid any
	if n.UserID != "" {
		uid = n.UserID
	}
	var tid any
	if n.TaskID != nil {
		tid = *n.TaskID
	}
	result, err := s.db.Exec(`INSERT INTO notifications
		(user_id, task_id, task_name, run_id, trigger_type, status, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, tid, n.TaskName, n.RunID, n.Trigger, n.Status, n.Content, time.Now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) List(userID string, limit, offset int) ([]Notification, int, error) {
	if limit <= 0 {
		limit = 50
	}

	where, args := userWhere(userID)

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM notifications WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, user_id, task_id, task_name, run_id, trigger_type, status, content, read, created_at
		FROM notifications WHERE ` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *n)
	}
	if out == nil {
		out = []Notification{}
	}
	return out, total, rows.Err()
}

func (s *Store) UnreadCount(userID string) (int, error) {
	where, args := userWhere(userID)
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM notifications WHERE read = 0 AND "+where, args...,
	).Scan(&count)
	return count, err
}

func (s *Store) MarkRead(id int64) error {
	_, err := s.db.Exec("UPDATE notifications SET read = 1 WHERE id = ?", id)
	return err
}

func (s *Store) MarkAllRead(userID string) error {
	where, args := userWhere(userID)
	_, err := s.db.Exec("UPDATE notifications SET read = 1 WHERE read = 0 AND "+where, args...)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM notifications WHERE id = ?", id)
	return err
}

func (s *Store) DeleteAll(userID string) error {
	where, args := userWhere(userID)
	_, err := s.db.Exec("DELETE FROM notifications WHERE "+where, args...)
	return err
}

func userWhere(userID string) (string, []any) {
	if userID == "" {
		return "user_id IS NULL", nil
	}
	return "(user_id = ? OR user_id IS NULL)", []any{userID}
}

func scanNotification(scanner interface{ Scan(...any) error }) (*Notification, error) {
	var n Notification
	var uid sql.NullString
	var tid sql.NullInt64
	err := scanner.Scan(&n.ID, &uid, &tid, &n.TaskName, &n.RunID,
		&n.Trigger, &n.Status, &n.Content, &n.Read, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.UserID = uid.String
	if tid.Valid {
		n.TaskID = &tid.Int64
	}
	return &n, nil
}
