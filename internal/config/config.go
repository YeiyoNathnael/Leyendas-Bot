package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime application configuration.
type Config struct {
	// Telegram
	TelegramBotToken    string
	TelegramGroupChatID int64

	// Turso (libSQL)
	TursoDatabaseURL string
	TursoAuthToken   string

	// Notion
	NotionToken        string
	NotionTasksDBID    string
	NotionTeamDBID     string
	NotionMeetingsDBID string

	// Google GenAI / Gemma 4 31B
	GenAIAPIKey    string
	GemmaModelName string

	// Google Calendar
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	GoogleRefreshToken string

	// Scheduling
	StandupCron           string
	StandupDigestCron     string
	NotionPollInterval    time.Duration
	DeadlineCheckInterval time.Duration
}

// Load reads configuration from .env and environment variables.
func Load() (*Config, error) {
	// Load .env if available, ignore error if missing (e.g. in containerized or CI environment)
	_ = godotenv.Load()

	cfg := &Config{
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TursoDatabaseURL:    os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:      os.Getenv("TURSO_AUTH_TOKEN"),
		NotionToken:         os.Getenv("NOTION_TOKEN"),
		NotionTasksDBID:     os.Getenv("NOTION_TASKS_DB_ID"),
		NotionTeamDBID:      os.Getenv("NOTION_TEAM_DB_ID"),
		NotionMeetingsDBID:  os.Getenv("NOTION_MEETINGS_DB_ID"),
		GenAIAPIKey:         os.Getenv("GEMINI_API_KEY"),
		GemmaModelName:      getEnvDefault("GEMMA_MODEL_NAME", "gemma-4-31b"),
		GoogleClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:   getEnvDefault("GOOGLE_REDIRECT_URL", "http://localhost:8085/oauth/callback"),
		GoogleRefreshToken:  os.Getenv("GOOGLE_REFRESH_TOKEN"),
		StandupCron:         getEnvDefault("STANDUP_CRON", "0 9 * * 1-5"),
		StandupDigestCron:   getEnvDefault("STANDUP_DIGEST_CRON", "0 15 * * 1-5"),
		NotionPollInterval:  getDurationDefault("NOTION_POLL_INTERVAL", 5*time.Minute),
		DeadlineCheckInterval: getDurationDefault("DEADLINE_CHECK_INTERVAL", 15*time.Minute),
	}

	groupIDStr := os.Getenv("TELEGRAM_GROUP_CHAT_ID")
	if groupIDStr != "" {
		groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid TELEGRAM_GROUP_CHAT_ID: %w", err)
		}
		cfg.TelegramGroupChatID = groupID
	}

	return cfg, nil
}

func getEnvDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getDurationDefault(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}
