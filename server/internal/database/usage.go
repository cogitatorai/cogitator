package database

import "fmt"

// DailyStats holds aggregated token usage for a single day and model tier.
type DailyStats struct {
	Date      string `json:"date"`
	ModelTier string `json:"model_tier"`
	TokensIn  int64  `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
}

// RecordTokenUsage inserts a row into token_usage.
func (db *DB) RecordTokenUsage(tier, model string, tokensIn, tokensOut int, taskRunID *int64, sessionKey string, userID *string) error {
	_, err := db.Exec(
		`INSERT INTO token_usage (model_tier, model_name, tokens_in, tokens_out, task_run_id, session_key, user_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tier, model, tokensIn, tokensOut, taskRunID, sessionKey, userID,
	)
	return err
}

// TodayTokenUsage returns the total tokens (in + out) consumed today for a
// given tier. When userID is non-empty, results are scoped to that user.
func (db *DB) TodayTokenUsage(tier, userID string) (int64, error) {
	where := "WHERE model_tier = ? AND created_at >= date('now')"
	args := []any{tier}
	if userID != "" {
		where += " AND user_id = ?"
		args = append(args, userID)
	}
	var total int64
	err := db.QueryRow(
		`SELECT COALESCE(SUM(tokens_in + tokens_out), 0) FROM token_usage `+where, args...,
	).Scan(&total)
	return total, err
}

// DailyTokenStats returns per-day, per-tier token totals for the last N days.
// When userID is non-empty, results are scoped to that user.
func (db *DB) DailyTokenStats(days int, userID string) ([]DailyStats, error) {
	if days <= 0 {
		days = 14
	}

	where := "WHERE created_at >= date('now', ?)"
	args := []any{fmt.Sprintf("-%d days", days)}
	if userID != "" {
		where += " AND user_id = ?"
		args = append(args, userID)
	}

	rows, err := db.Query(
		`SELECT date(created_at) AS date, model_tier,
		        SUM(tokens_in)  AS tokens_in,
		        SUM(tokens_out) AS tokens_out
		 FROM token_usage
		 `+where+`
		 GROUP BY date, model_tier
		 ORDER BY date`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []DailyStats
	for rows.Next() {
		var s DailyStats
		if err := rows.Scan(&s.Date, &s.ModelTier, &s.TokensIn, &s.TokensOut); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}
