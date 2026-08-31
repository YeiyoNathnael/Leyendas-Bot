package bot

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"leyendas-bot/internal/ai"
	"leyendas-bot/internal/config"
	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
)

func TestBotEngineInitialization(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.NewStore("file:"+filepath.Join(tempDir, "test.db"), "")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	notionClient := notion.NewClient("token")
	executor := ai.NewExecutor(store, notionClient, nil, "tasks", "team", "meetings")

	cfg := &config.Config{
		TelegramBotToken:    fmt.Sprintf("%d:%s", 12345, "dummy_fixture"),
		TelegramGroupChatID: -100123456789,
	}

	engine, err := NewBotEngine(cfg, store, executor, nil)
	if err != nil {
		t.Fatalf("failed to init bot engine: %v", err)
	}

	if engine == nil || engine.dispatcher == nil {
		t.Fatal("expected non-nil bot engine and dispatcher")
	}

	// Test mention filter helper
	msg := &gotgbot.Message{
		Text: "Hello @LeyendasBot can you check my tasks?",
		Entities: []gotgbot.MessageEntity{
			{Type: "mention", Offset: 6, Length: 12},
		},
	}

	engine.bot.Username = "LeyendasBot"
	if !engine.botMentionFilter(msg) {
		t.Error("expected botMentionFilter to return true for matching mention")
	}

	nonMatchingMsg := &gotgbot.Message{
		Text: "Hello someone else",
	}
	if engine.botMentionFilter(nonMatchingMsg) {
		t.Error("expected botMentionFilter to return false for non-mention")
	}
}
