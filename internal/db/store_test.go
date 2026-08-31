package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	dsn := "file:" + dbPath

	store, err := NewStore(dsn, "")
	if err != nil {
		t.Fatalf("failed to initialize test store: %v", err)
	}
	return store
}

func TestTeamMembersCRUD(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()

	member := &TeamMember{
		NotionPageID:     "notion-page-123",
		FullName:         "Nathnael Workneh",
		TelegramUserID:   987654321,
		TelegramUsername: "yeiyo",
		GoogleEmail:      "yeiyo@example.com",
		RoleModule:       "Lead",
	}

	// 1. Upsert
	if err := store.UpsertMember(ctx, member); err != nil {
		t.Fatalf("UpsertMember failed: %v", err)
	}

	// 2. Query by Telegram ID
	found, err := store.GetMemberByTelegramID(ctx, 987654321)
	if err != nil {
		t.Fatalf("GetMemberByTelegramID failed: %v", err)
	}
	if found == nil || found.FullName != "Nathnael Workneh" || found.GoogleEmail != "yeiyo@example.com" {
		t.Fatalf("unexpected member data: %+v", found)
	}

	// 3. Query by Email
	byEmail, err := store.GetMemberByEmail(ctx, "YEIYO@example.com")
	if err != nil || byEmail == nil {
		t.Fatalf("GetMemberByEmail failed: %v, member: %+v", err, byEmail)
	}

	// 4. Query by Notion Page ID
	byPageID, err := store.GetMemberByNotionPageID(ctx, "notion-page-123")
	if err != nil || byPageID == nil {
		t.Fatalf("GetMemberByNotionPageID failed: %v, member: %+v", err, byPageID)
	}

	// 5. List
	all, err := store.ListMembers(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListMembers failed: %v, count: %d", err, len(all))
	}
}

func TestRemindersAndSnapshots(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	taskID := "task-uuid-abc"

	// 1. Snapshot test
	snap := &TaskSnapshot{
		TaskID:          taskID,
		LastStatus:      "In progress",
		LastEditedTime:  time.Now().UTC().Format(time.RFC3339),
		NotifiedDone:    false,
		NotifiedCreated: true,
	}
	if err := store.UpsertTaskSnapshot(ctx, snap); err != nil {
		t.Fatalf("UpsertTaskSnapshot failed: %v", err)
	}

	fetchedSnap, err := store.GetTaskSnapshot(ctx, taskID)
	if err != nil || fetchedSnap == nil || fetchedSnap.LastStatus != "In progress" {
		t.Fatalf("GetTaskSnapshot failed: %v, snap: %+v", err, fetchedSnap)
	}

	// 2. Idempotent Reminder Lock
	firstAttempt, err := store.TryRecordReminder(ctx, taskID, "48h")
	if err != nil || !firstAttempt {
		t.Fatalf("expected first reminder attempt to succeed, got %v, err: %v", firstAttempt, err)
	}

	secondAttempt, err := store.TryRecordReminder(ctx, taskID, "48h")
	if err != nil || secondAttempt {
		t.Fatalf("expected duplicate reminder attempt to return false, got %v, err: %v", secondAttempt, err)
	}

	// Different reminder type should succeed
	dueAttempt, err := store.TryRecordReminder(ctx, taskID, "due_date")
	if err != nil || !dueAttempt {
		t.Fatalf("expected different reminder type to succeed, got %v, err: %v", dueAttempt, err)
	}
}

func TestStandupRoundsAndReplies(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	ctx := context.Background()
	today := time.Now().Format("2006-01-02")

	// 1. Create Round
	round, err := store.GetOrCreateTodayStandupRound(ctx, today, 1001)
	if err != nil || round == nil {
		t.Fatalf("GetOrCreateTodayStandupRound failed: %v", err)
	}

	// 2. Record User Reply
	if err := store.RecordStandupReply(ctx, round.ID, 987654321, "Nathnael", "1. Auth done 2. Bot tests"); err != nil {
		t.Fatalf("RecordStandupReply failed: %v", err)
	}

	// 3. Fetch Replies
	replies, err := store.GetStandupRepliesForRound(ctx, round.ID)
	if err != nil || len(replies) != 1 {
		t.Fatalf("GetStandupRepliesForRound failed: %v, count: %d", err, len(replies))
	}
	if replies[0].ReplyText != "1. Auth done 2. Bot tests" {
		t.Fatalf("unexpected reply text: %s", replies[0].ReplyText)
	}

	// 4. Close Round
	if err := store.CloseStandupRound(ctx, round.ID, 2002); err != nil {
		t.Fatalf("CloseStandupRound failed: %v", err)
	}

	closedRound, err := store.GetActiveStandupRound(ctx, today)
	if err != nil || closedRound == nil || !closedRound.IsClosed || closedRound.SummaryMessageID != 2002 {
		t.Fatalf("unexpected closed round state: %+v, err: %v", closedRound, err)
	}
}
