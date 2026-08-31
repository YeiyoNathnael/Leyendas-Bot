package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TeamMember represents a registered team member in the Turso mirror.
type TeamMember struct {
	ID               int64     `json:"id"`
	NotionPageID     string    `json:"notion_page_id"`
	FullName         string    `json:"full_name"`
	TelegramUserID   int64     `json:"telegram_user_id"`
	TelegramUsername string    `json:"telegram_username"`
	GoogleEmail      string    `json:"google_email"`
	RoleModule       string    `json:"role_module"`
	CreatedAt        time.Time `json:"created_at"`
}

// GetMemberByTelegramID looks up a registered member by their Telegram user ID.
func (s *Store) GetMemberByTelegramID(ctx context.Context, telegramID int64) (*TeamMember, error) {
	query := `
		SELECT id, notion_page_id, full_name, telegram_user_id, telegram_username, google_email, role_module, created_at
		FROM team_members
		WHERE telegram_user_id = ?
		LIMIT 1;
	`
	row := s.DB.QueryRowContext(ctx, query, telegramID)
	return scanMember(row)
}

// GetMemberByEmail looks up a registered member by their Google email address.
func (s *Store) GetMemberByEmail(ctx context.Context, email string) (*TeamMember, error) {
	query := `
		SELECT id, notion_page_id, full_name, telegram_user_id, telegram_username, google_email, role_module, created_at
		FROM team_members
		WHERE LOWER(google_email) = LOWER(?)
		LIMIT 1;
	`
	row := s.DB.QueryRowContext(ctx, query, email)
	return scanMember(row)
}

// GetMemberByNotionPageID looks up a registered member by their Notion The Team page ID.
func (s *Store) GetMemberByNotionPageID(ctx context.Context, notionPageID string) (*TeamMember, error) {
	query := `
		SELECT id, notion_page_id, full_name, telegram_user_id, telegram_username, google_email, role_module, created_at
		FROM team_members
		WHERE notion_page_id = ?
		LIMIT 1;
	`
	row := s.DB.QueryRowContext(ctx, query, notionPageID)
	return scanMember(row)
}

// ListMembers returns all registered team members.
func (s *Store) ListMembers(ctx context.Context) ([]*TeamMember, error) {
	query := `
		SELECT id, notion_page_id, full_name, telegram_user_id, telegram_username, google_email, role_module, created_at
		FROM team_members
		ORDER BY full_name ASC;
	`
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query members: %w", err)
	}
	defer rows.Close()

	var members []*TeamMember
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		if m != nil {
			members = append(members, m)
		}
	}

	return members, rows.Err()
}

// UpsertMember inserts or updates a team member record in the Turso mirror.
func (s *Store) UpsertMember(ctx context.Context, m *TeamMember) error {
	query := `
		INSERT INTO team_members (notion_page_id, full_name, telegram_user_id, telegram_username, google_email, role_module)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(telegram_user_id) DO UPDATE SET
			notion_page_id = excluded.notion_page_id,
			full_name = excluded.full_name,
			telegram_username = excluded.telegram_username,
			google_email = excluded.google_email,
			role_module = excluded.role_module;
	`
	_, err := s.DB.ExecContext(ctx, query,
		m.NotionPageID,
		m.FullName,
		m.TelegramUserID,
		m.TelegramUsername,
		m.GoogleEmail,
		m.RoleModule,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert member: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMember(s rowScanner) (*TeamMember, error) {
	var m TeamMember
	var username, role sql.NullString
	var createdAt any // scan as string/time.Time safely

	err := s.Scan(
		&m.ID,
		&m.NotionPageID,
		&m.FullName,
		&m.TelegramUserID,
		&username,
		&m.GoogleEmail,
		&role,
		&createdAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	m.TelegramUsername = username.String
	m.RoleModule = role.String

	switch v := createdAt.(type) {
	case time.Time:
		m.CreatedAt = v
	case string:
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			m.CreatedAt = t
		}
	}

	return &m, nil
}
