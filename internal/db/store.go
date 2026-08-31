package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

// Store encapsulates the database connection and operational repositories.
type Store struct {
	DB *sql.DB
}

// NewStore initializes a new Store instance and executes automatic schema migrations.
func NewStore(dbURL, authToken string) (*Store, error) {
	var dsn string
	if authToken != "" {
		dsn = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	} else {
		dsn = dbURL
	}

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Optimize connection pooling for Turso / libSQL
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &Store{DB: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to execute migrations: %w", err)
	}

	return store, nil
}

// Close gracefully closes the underlying database pool.
func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS team_members (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		notion_page_id     TEXT NOT NULL UNIQUE,
		full_name          TEXT NOT NULL,
		telegram_user_id   INTEGER NOT NULL UNIQUE,
		telegram_username  TEXT,
		google_email       TEXT NOT NULL,
		role_module        TEXT,
		created_at         TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS task_snapshots (
		task_id            TEXT PRIMARY KEY,
		last_status        TEXT NOT NULL,
		last_edited_time   TEXT NOT NULL,
		notified_done      BOOLEAN DEFAULT 0,
		notified_created   BOOLEAN DEFAULT 0,
		synced_at          TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS reminder_state (
		task_id            TEXT NOT NULL,
		reminder_type      TEXT NOT NULL,
		sent_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (task_id, reminder_type)
	);

	CREATE TABLE IF NOT EXISTS standup_rounds (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		round_date         TEXT NOT NULL UNIQUE,
		prompt_message_id  INTEGER,
		summary_message_id INTEGER,
		is_closed          BOOLEAN DEFAULT 0,
		created_at         TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS standup_replies (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		round_id           INTEGER NOT NULL REFERENCES standup_rounds(id),
		telegram_user_id   INTEGER NOT NULL,
		full_name          TEXT NOT NULL,
		reply_text         TEXT NOT NULL,
		submitted_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(round_id, telegram_user_id)
	);
	`
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}
