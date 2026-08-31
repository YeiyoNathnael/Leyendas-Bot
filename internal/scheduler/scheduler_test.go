package scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"leyendas-bot/internal/config"
	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
)

func TestSchedulerLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.NewStore("file:"+filepath.Join(tempDir, "test.db"), "")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	notionClient := notion.NewClient("token")

	cfg := &config.Config{
		StandupCron:           "0 9 * * 1-5",
		StandupDigestCron:     "0 15 * * 1-5",
		NotionPollInterval:    1 * time.Minute,
		DeadlineCheckInterval: 5 * time.Minute,
		TelegramGroupChatID:   -100123456789,
	}

	bot := &gotgbot.Bot{
		User: gotgbot.User{
			Username: "TestBot",
		},
	}

	sched := NewScheduler(bot, store, notionClient, cfg)
	if sched == nil || sched.cron == nil {
		t.Fatal("expected non-nil scheduler and cron instance")
	}

	sched.Start()
	time.Sleep(10 * time.Millisecond)
	sched.Stop()
}
