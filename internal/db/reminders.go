package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TaskSnapshot represents the stored state of a Notion task to avoid repeating notifications upon polling.
type TaskSnapshot struct {
	TaskID          string    `json:"task_id"`
	LastStatus      string    `json:"last_status"`
	LastEditedTime  string    `json:"last_edited_time"`
	NotifiedDone    bool      `json:"notified_done"`
	NotifiedCreated bool      `json:"notified_created"`
	SyncedAt        time.Time `json:"synced_at"`
}

// GetTaskSnapshot retrieves the last recorded snapshot of a task.
func (s *Store) GetTaskSnapshot(ctx context.Context, taskID string) (*TaskSnapshot, error) {
	query := `
		SELECT task_id, last_status, last_edited_time, notified_done, notified_created, synced_at
		FROM task_snapshots
		WHERE task_id = ?
		LIMIT 1;
	`
	row := s.DB.QueryRowContext(ctx, query, taskID)

	var snap TaskSnapshot
	var syncedAt any
	err := row.Scan(
		&snap.TaskID,
		&snap.LastStatus,
		&snap.LastEditedTime,
		&snap.NotifiedDone,
		&snap.NotifiedCreated,
		&syncedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get task snapshot: %w", err)
	}

	return &snap, nil
}

// UpsertTaskSnapshot saves or updates the snapshot state of a task.
func (s *Store) UpsertTaskSnapshot(ctx context.Context, snap *TaskSnapshot) error {
	query := `
		INSERT INTO task_snapshots (task_id, last_status, last_edited_time, notified_done, notified_created, synced_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task_id) DO UPDATE SET
			last_status = excluded.last_status,
			last_edited_time = excluded.last_edited_time,
			notified_done = excluded.notified_done,
			notified_created = excluded.notified_created,
			synced_at = CURRENT_TIMESTAMP;
	`
	_, err := s.DB.ExecContext(ctx, query,
		snap.TaskID,
		snap.LastStatus,
		snap.LastEditedTime,
		snap.NotifiedDone,
		snap.NotifiedCreated,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert task snapshot: %w", err)
	}
	return nil
}

// TryRecordReminder attempts to atomically mark a reminder as sent.
// Returns true if this is the first time the reminder is sent for this task and type.
// Returns false if it was already recorded previously.
func (s *Store) TryRecordReminder(ctx context.Context, taskID, reminderType string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if already sent
	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM reminder_state WHERE task_id = ? AND reminder_type = ?`,
		taskID, reminderType,
	).Scan(&exists)

	if err == nil {
		return false, nil // Already sent
	} else if err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to query reminder state: %w", err)
	}

	// Record reminder
	_, err = tx.ExecContext(ctx,
		`INSERT INTO reminder_state (task_id, reminder_type) VALUES (?, ?)`,
		taskID, reminderType,
	)
	if err != nil {
		return false, fmt.Errorf("failed to insert reminder record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit reminder tx: %w", err)
	}

	return true, nil
}
