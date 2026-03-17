package app

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/gemini"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/integrations"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/rag"
	"github.com/tmc/langchaingo/schema"
)

type ChatService struct {
	ragService *rag.Service
	model      *gemini.Client
	db         *sql.DB
}

type PendingQuestion struct {
	ID        string
	ChatID    int64
	MessageID int64
	UserID    int64
	Question  string
	CreatedAt time.Time
}

func NewChatService(database *sql.DB, ragService *rag.Service, model *gemini.Client) *ChatService {
	return &ChatService{ragService: ragService, model: model, db: database}
}

func (s *ChatService) RecordChatSession(ctx context.Context, chatID int64, userID int64) {
	if s.db == nil || chatID == 0 || userID == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO chat_sessions (id, telegram_chat_id, telegram_user_id, started_at, last_activity_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  last_activity_at = excluded.last_activity_at
`, fmt.Sprintf("%d:%d", chatID, userID), fmt.Sprintf("%d", chatID), fmt.Sprintf("%d", userID), now, now)
}

func (s *ChatService) EnqueuePendingQuestion(ctx context.Context, chatID, messageID, userID int64, question string) {
	if s.db == nil || chatID == 0 || messageID == 0 || userID == 0 || strings.TrimSpace(question) == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := fmt.Sprintf("%d:%d", chatID, messageID)
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO pending_questions (id, telegram_chat_id, telegram_message_id, telegram_user_id, question, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING
`, id, fmt.Sprintf("%d", chatID), fmt.Sprintf("%d", messageID), fmt.Sprintf("%d", userID), strings.TrimSpace(question), now)
}

func (s *ChatService) MarkPendingQuestionAnswered(ctx context.Context, chatID, messageID int64) {
	if s.db == nil || chatID == 0 || messageID == 0 {
		return
	}
	_, _ = s.db.ExecContext(ctx, `
UPDATE pending_questions
SET answered_at = ?
WHERE telegram_chat_id = ? AND telegram_message_id = ? AND answered_at IS NULL
`, time.Now().UTC().Format(time.RFC3339), fmt.Sprintf("%d", chatID), fmt.Sprintf("%d", messageID))
}

func (s *ChatService) ListDuePendingQuestions(ctx context.Context, minAge time.Duration, limit int) []PendingQuestion {
	if s.db == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	cutoff := time.Now().UTC().Add(-minAge).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, telegram_chat_id, telegram_message_id, telegram_user_id, question, created_at
FROM pending_questions
WHERE answered_at IS NULL
  AND dispatched_at IS NULL
  AND created_at <= ?
ORDER BY created_at ASC
LIMIT ?
`, cutoff, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]PendingQuestion, 0)
	for rows.Next() {
		var q PendingQuestion
		var chatIDRaw, messageIDRaw, userIDRaw, createdRaw string
		if err := rows.Scan(&q.ID, &chatIDRaw, &messageIDRaw, &userIDRaw, &q.Question, &createdRaw); err != nil {
			continue
		}
		q.ChatID, _ = strconv.ParseInt(chatIDRaw, 10, 64)
		q.MessageID, _ = strconv.ParseInt(messageIDRaw, 10, 64)
		q.UserID, _ = strconv.ParseInt(userIDRaw, 10, 64)
		q.CreatedAt, _ = time.Parse(time.RFC3339, createdRaw)
		out = append(out, q)
	}
	return out
}

func (s *ChatService) MarkPendingQuestionDispatched(ctx context.Context, id string) {
	if s.db == nil || strings.TrimSpace(id) == "" {
		return
	}
	_, _ = s.db.ExecContext(ctx, `
UPDATE pending_questions
SET dispatched_at = ?
WHERE id = ?
`, time.Now().UTC().Format(time.RFC3339), id)
}

func (s *ChatService) Answer(ctx context.Context, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "Please send a question after /ask.", nil
	}

	docs, err := s.ragService.Search(ctx, question, 4)
	if err != nil {
		return "", fmt.Errorf("retrieval failed: %w", err)
	}
	if len(docs) == 0 {
		return "I do not have enough indexed context yet. Run /sync after connecting your sources.", nil
	}

	var contextBuilder strings.Builder
	for i, doc := range docs {
		title, _ := doc.Metadata["title"].(string)
		if title == "" {
			title = "Untitled Source"
		}
		contextBuilder.WriteString(fmt.Sprintf("[%d] %s\n%s\n\n", i+1, title, doc.PageContent))
	}

	prompt := fmt.Sprintf(
		"You are Takhtit's project assistant in Telegram. Use only the provided context when possible. If context is insufficient, state what is missing briefly. Keep under 8 sentences.\n\nContext:\n%s\nQuestion:\n%s\n\nReturn a concise answer with brief source references like [1], [2].",
		contextBuilder.String(),
		question,
	)

	if s.model == nil {
		return "RAG is initialized but Gemini client is missing. Configure GEMINI_API_KEY.", nil
	}

	answer, err := s.model.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("model generation failed: %w", err)
	}
	return strings.TrimSpace(answer) + formatSourceCitations(docs), nil
}

func formatSourceCitations(docs []schema.Document) string {
	if len(docs) == 0 {
		return ""
	}
	seen := map[string]struct{}{}
	lines := make([]string, 0, len(docs))
	for _, doc := range docs {
		title, _ := doc.Metadata["title"].(string)
		url, _ := doc.Metadata["url"].(string)
		sourceID, _ := doc.Metadata["source_id"].(string)
		if strings.TrimSpace(title) == "" {
			title = sourceID
		}
		key := strings.TrimSpace(title) + "|" + strings.TrimSpace(url)
		if key == "|" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		label := escapeMarkdownV2(strings.TrimSpace(title))
		if strings.TrimSpace(url) != "" {
			label += " \\- " + escapeMarkdownV2(strings.TrimSpace(url))
		}
		lines = append(lines, fmt.Sprintf("%d\\. %s", len(lines)+1, label))
		if len(lines) >= 4 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nSources:\n" + strings.Join(lines, "\n")
}

func escapeMarkdownV2(s string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(s)
}

type IntegrationService struct {
	svc *integrations.Service
}

func NewIntegrationService(database *sql.DB) *IntegrationService {
	return &IntegrationService{svc: integrations.NewService(database)}
}

func (s *IntegrationService) GitHubConnect(telegramUserID int64) string {
	return s.svc.GitHubConnectURL(telegramUserID)
}

func (s *IntegrationService) GoogleConnect(telegramUserID int64) string {
	return s.svc.GoogleConnectURL(telegramUserID)
}

func (s *IntegrationService) GitHubOAuthCallback(ctx context.Context, code, state string) (int64, error) {
	return s.svc.HandleGitHubCallback(ctx, code, state)
}

func (s *IntegrationService) GoogleOAuthCallback(ctx context.Context, code, state string) (int64, error) {
	return s.svc.HandleGoogleCallback(ctx, code, state)
}

type SyncService struct {
	integrations *IntegrationService
	ragService   *rag.Service
	db           *sql.DB
	mu           sync.RWMutex
	lastRun      map[int64]SyncRun
	metrics      SyncMetrics
}

type SyncRun struct {
	StartedAt   time.Time
	CompletedAt time.Time
	Status      string
	Message     string
}

type SyncMetrics struct {
	TotalRuns      int64
	SuccessfulRuns int64
	FailedRuns     int64
	LastStatus     string
	LastMessage    string
	LastStartedAt  time.Time
	LastEndedAt    time.Time
}

func NewSyncService(database *sql.DB, integrations *IntegrationService, ragService *rag.Service) *SyncService {
	return &SyncService{
		integrations: integrations,
		ragService:   ragService,
		db:           database,
		lastRun:      make(map[int64]SyncRun),
	}
}

func (s *SyncService) TriggerForUser(ctx context.Context, userID int64) string {
	startedAt := time.Now().UTC()
	s.setRun(userID, SyncRun{StartedAt: startedAt, Status: "running", Message: "sync started"})
	s.persistSyncRun(ctx, userID, SyncRun{StartedAt: startedAt, Status: "running", Message: "sync started"})
	s.recordSyncStart(startedAt)

	externalDocs, err := s.integrations.svc.CollectAll(ctx, userID)
	if err != nil {
		msg := fmt.Sprintf("Sync failed: %v", err)
		run := SyncRun{StartedAt: startedAt, CompletedAt: time.Now().UTC(), Status: "failed", Message: msg}
		s.setRun(userID, run)
		s.persistSyncRun(ctx, userID, run)
		s.recordSyncEnd(run)
		return msg
	}

	inputDocs := make([]rag.InputDocument, 0, len(externalDocs))
	for _, doc := range externalDocs {
		inputDocs = append(inputDocs, rag.InputDocument{
			SourceID: doc.SourceID,
			Title:    doc.Title,
			Content:  doc.Content,
			URL:      doc.URL,
			Tags:     doc.Tags,
		})
	}

	chunkCount, err := s.ragService.ReplaceIndex(ctx, inputDocs)
	if err != nil {
		msg := fmt.Sprintf("Sync failed while indexing: %v", err)
		run := SyncRun{StartedAt: startedAt, CompletedAt: time.Now().UTC(), Status: "failed", Message: msg}
		s.setRun(userID, run)
		s.persistSyncRun(ctx, userID, run)
		s.recordSyncEnd(run)
		return msg
	}

	msg := fmt.Sprintf("Sync completed: %d source docs indexed into %d chunks.", len(inputDocs), chunkCount)
	run := SyncRun{StartedAt: startedAt, CompletedAt: time.Now().UTC(), Status: "completed", Message: msg}
	s.setRun(userID, run)
	s.persistSyncRun(ctx, userID, run)
	s.recordSyncEnd(run)
	return msg
}

func (s *SyncService) StatusForUser(userID int64) string {
	s.mu.RLock()
	run, ok := s.lastRun[userID]
	s.mu.RUnlock()
	if !ok {
		run, ok = s.latestPersistedRun(userID)
		if !ok {
			return "No sync has been run for your account yet. Use /sync first."
		}
	}

	started := run.StartedAt.Format(time.RFC3339)
	completed := "-"
	if !run.CompletedAt.IsZero() {
		completed = run.CompletedAt.Format(time.RFC3339)
	}

	return fmt.Sprintf("Last sync status: %s\nStarted: %s\nCompleted: %s\nDetails: %s", run.Status, started, completed, run.Message)
}

func (s *SyncService) setRun(userID int64, run SyncRun) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if previous, ok := s.lastRun[userID]; ok && run.StartedAt.IsZero() {
		run.StartedAt = previous.StartedAt
	}
	s.lastRun[userID] = run
}

func (s *SyncService) persistSyncRun(ctx context.Context, userID int64, run SyncRun) {
	if s.db == nil || userID == 0 {
		return
	}
	startedAt := run.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	completedAt := ""
	if !run.CompletedAt.IsZero() {
		completedAt = run.CompletedAt.UTC().Format(time.RFC3339)
	}
	_, _ = s.db.ExecContext(ctx, `
INSERT INTO sync_jobs (id, telegram_user_id, status, started_at, completed_at, message)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  status = excluded.status,
  completed_at = excluded.completed_at,
  message = excluded.message
`, fmt.Sprintf("%d:%s", userID, startedAt.UTC().Format(time.RFC3339Nano)), fmt.Sprintf("%d", userID), run.Status, startedAt.UTC().Format(time.RFC3339), completedAt, run.Message)
}

func (s *SyncService) latestPersistedRun(userID int64) (SyncRun, bool) {
	if s.db == nil || userID == 0 {
		return SyncRun{}, false
	}
	row := s.db.QueryRow(`
SELECT status, started_at, completed_at, message
FROM sync_jobs
WHERE telegram_user_id = ?
ORDER BY started_at DESC
LIMIT 1
`, fmt.Sprintf("%d", userID))

	var status, startedAtRaw, completedAtRaw, message string
	if err := row.Scan(&status, &startedAtRaw, &completedAtRaw, &message); err != nil {
		return SyncRun{}, false
	}
	startedAt, _ := time.Parse(time.RFC3339, startedAtRaw)
	completedAt := time.Time{}
	if strings.TrimSpace(completedAtRaw) != "" {
		completedAt, _ = time.Parse(time.RFC3339, completedAtRaw)
	}
	return SyncRun{StartedAt: startedAt, CompletedAt: completedAt, Status: status, Message: message}, true
}

func (s *SyncService) recordSyncStart(startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics.TotalRuns++
	s.metrics.LastStartedAt = startedAt
	s.metrics.LastStatus = "running"
	s.metrics.LastMessage = "sync started"
}

func (s *SyncService) recordSyncEnd(run SyncRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.Status == "completed" {
		s.metrics.SuccessfulRuns++
	}
	if run.Status == "failed" {
		s.metrics.FailedRuns++
	}
	s.metrics.LastStatus = run.Status
	s.metrics.LastMessage = run.Message
	s.metrics.LastEndedAt = run.CompletedAt
}

func (s *SyncService) MetricsSnapshot() SyncMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}
