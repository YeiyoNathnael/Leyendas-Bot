package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StandupRound represents a daily standup round.
type StandupRound struct {
	ID               int64     `json:"id"`
	RoundDate        string    `json:"round_date"` // YYYY-MM-DD
	PromptMessageID  int64     `json:"prompt_message_id"`
	SummaryMessageID int64     `json:"summary_message_id"`
	IsClosed         bool      `json:"is_closed"`
	CreatedAt        time.Time `json:"created_at"`
}

// StandupReply represents a team member's response in a standup thread.
type StandupReply struct {
	ID             int64     `json:"id"`
	RoundID        int64     `json:"round_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	FullName       string    `json:"full_name"`
	ReplyText      string    `json:"reply_text"`
	SubmittedAt    time.Time `json:"submitted_at"`
}

// GetOrCreateTodayStandupRound retrieves an existing round for today or inserts a new one.
func (s *Store) GetOrCreateTodayStandupRound(ctx context.Context, dateStr string, promptMsgID int64) (*StandupRound, error) {
	query := `
		INSERT INTO standup_rounds (round_date, prompt_message_id)
		VALUES (?, ?)
		ON CONFLICT(round_date) DO UPDATE SET
			prompt_message_id = CASE WHEN excluded.prompt_message_id > 0 THEN excluded.prompt_message_id ELSE standup_rounds.prompt_message_id END
		RETURNING id, round_date, prompt_message_id, summary_message_id, is_closed, created_at;
	`
	row := s.DB.QueryRowContext(ctx, query, dateStr, promptMsgID)

	var r StandupRound
	var promptID, summaryID sql.NullInt64
	var createdAt any
	err := row.Scan(&r.ID, &r.RoundDate, &promptID, &summaryID, &r.IsClosed, &createdAt)
	if err != nil {
		// Fallback for drivers that don't support RETURNING
		return s.GetActiveStandupRound(ctx, dateStr)
	}

	r.PromptMessageID = promptID.Int64
	r.SummaryMessageID = summaryID.Int64
	return &r, nil
}

// GetActiveStandupRound returns the standup round for a given date.
func (s *Store) GetActiveStandupRound(ctx context.Context, dateStr string) (*StandupRound, error) {
	query := `
		SELECT id, round_date, prompt_message_id, summary_message_id, is_closed, created_at
		FROM standup_rounds
		WHERE round_date = ?
		LIMIT 1;
	`
	row := s.DB.QueryRowContext(ctx, query, dateStr)

	var r StandupRound
	var promptID, summaryID sql.NullInt64
	var createdAt any
	err := row.Scan(&r.ID, &r.RoundDate, &promptID, &summaryID, &r.IsClosed, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query active standup round: %w", err)
	}

	r.PromptMessageID = promptID.Int64
	r.SummaryMessageID = summaryID.Int64
	return &r, nil
}

// RecordStandupReply records or updates a member's standup reply.
func (s *Store) RecordStandupReply(ctx context.Context, roundID, telegramUserID int64, fullName, replyText string) error {
	query := `
		INSERT INTO standup_replies (round_id, telegram_user_id, full_name, reply_text)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(round_id, telegram_user_id) DO UPDATE SET
			full_name = excluded.full_name,
			reply_text = excluded.reply_text,
			submitted_at = CURRENT_TIMESTAMP;
	`
	_, err := s.DB.ExecContext(ctx, query, roundID, telegramUserID, fullName, replyText)
	if err != nil {
		return fmt.Errorf("failed to record standup reply: %w", err)
	}
	return nil
}

// GetStandupRepliesForRound retrieves all replies submitted for a specific standup round.
func (s *Store) GetStandupRepliesForRound(ctx context.Context, roundID int64) ([]*StandupReply, error) {
	query := `
		SELECT id, round_id, telegram_user_id, full_name, reply_text, submitted_at
		FROM standup_replies
		WHERE round_id = ?
		ORDER BY submitted_at ASC;
	`
	rows, err := s.DB.QueryContext(ctx, query, roundID)
	if err != nil {
		return nil, fmt.Errorf("failed to query standup replies: %w", err)
	}
	defer rows.Close()

	var replies []*StandupReply
	for rows.Next() {
		var rep StandupReply
		var subAt any
		if err := rows.Scan(&rep.ID, &rep.RoundID, &rep.TelegramUserID, &rep.FullName, &rep.ReplyText, &subAt); err != nil {
			return nil, err
		}
		replies = append(replies, &rep)
	}

	return replies, rows.Err()
}

// CloseStandupRound marks the round as closed and attaches the summary message ID.
func (s *Store) CloseStandupRound(ctx context.Context, roundID int64, summaryMsgID int64) error {
	query := `
		UPDATE standup_rounds
		SET is_closed = 1, summary_message_id = ?
		WHERE id = ?;
	`
	_, err := s.DB.ExecContext(ctx, query, summaryMsgID, roundID)
	if err != nil {
		return fmt.Errorf("failed to close standup round: %w", err)
	}
	return nil
}
