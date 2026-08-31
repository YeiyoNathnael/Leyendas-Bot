package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/oauth2"

	"leyendas-bot/internal/ai"
	"leyendas-bot/internal/bot"
	"leyendas-bot/internal/calendar"
	"leyendas-bot/internal/config"
	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
	"leyendas-bot/internal/scheduler"
)

func main() {
	log.Println("🚀 Starting Leyendas Bot Daemon...")

	// 1. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[FATAL] Failed to load configuration: %v", err)
	}

	if cfg.TelegramBotToken == "" {
		log.Fatal("[FATAL] TELEGRAM_BOT_TOKEN is required in .env or environment")
	}

	// 2. Initialize Database (Turso libSQL mirror)
	dbURL := cfg.TursoDatabaseURL
	if dbURL == "" {
		dbURL = "file:leyendas.db"
		log.Println("[INFO] TURSO_DATABASE_URL not set; defaulting to local SQLite file: leyendas.db")
	}

	store, err := db.NewStore(dbURL, cfg.TursoAuthToken)
	if err != nil {
		log.Fatalf("[FATAL] Database initialization failed: %v", err)
	}
	defer store.Close()
	log.Println("✅ Database connected and migrations applied")

	// 3. Initialize Notion Client
	notionClient := notion.NewClient(cfg.NotionToken)
	log.Println("✅ Rate-limited Notion client initialized")

	// 4. Initialize Google Calendar Service (Optional on first run until CLI auth is executed)
	var calService *calendar.Service
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.GoogleRefreshToken != "" {
		calCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var calErr error
		calService, calErr = calendar.NewService(calCtx, cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRefreshToken, func(token *oauth2.Token) {
			log.Println("[INFO] Google Calendar OAuth token refreshed automatically")
		})
		if calErr != nil {
			log.Printf("[WARN] Google Calendar init warning: %v (scheduling features will be limited)", calErr)
		} else {
			log.Println("✅ Google Calendar & Meet service initialized")
		}
	} else {
		log.Println("[INFO] Google Calendar credentials not fully set. Run 'go run ./cmd/auth' to configure.")
	}

	// 5. Initialize Tool Executor & Gemma 4 31B AI Agent
	executor := ai.NewExecutor(store, notionClient, calService, cfg.NotionTasksDBID, cfg.NotionTeamDBID, cfg.NotionMeetingsDBID)

	aiCtx, aiCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer aiCancel()

	aiAgent, err := ai.NewAgent(aiCtx, cfg.GenAIAPIKey, cfg.GemmaModelName, executor)
	if err != nil {
		log.Printf("[WARN] AI Agent init warning: %v (Gemma tool calling will be disabled)", err)
	} else {
		log.Printf("✅ Gemma 4 31B AI Agent initialized (model: %s)", cfg.GemmaModelName)
	}

	// 6. Initialize Telegram Bot Engine
	engine, err := bot.NewBotEngine(cfg, store, executor, aiAgent)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize Telegram Bot Engine: %v", err)
	}

	// 7. Initialize & Start Background Scheduler
	sched := scheduler.NewScheduler(engine.Bot(), store, notionClient, cfg)
	sched.Start()

	// Seed task snapshot baseline so historical tasks aren't spammed
	go func() {
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer initCancel()
		sched.TriggerInitialSync(initCtx)
	}()

	// 8. Start Telegram Long Polling
	if err := engine.Start(); err != nil {
		log.Fatalf("[FATAL] Failed to start Telegram polling: %v", err)
	}

	// 9. Graceful Shutdown on SIGINT / SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	sig := <-sigChan

	log.Printf("🛑 Received shutdown signal (%s). Cleaning up...", sig)

	// Stop polling & dispatcher
	if err := engine.Stop(); err != nil {
		log.Printf("[ERROR] Error stopping bot engine: %v", err)
	}

	// Stop background scheduler
	sched.Stop()

	log.Println("👋 Leyendas Bot daemon shut down cleanly.")
}
