package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/app"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/db"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/gemini"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/httpserver"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/rag"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/telegram"
	"github.com/joho/godotenv"
)

const botUserName = "@takhtit_assistant_bot"

func main() {
	err := godotenv.Load()
	token := os.Getenv("TOKEN")
	if token == "" {
		panic("TOKEN environment variable is empty")
	}

	geminiClient, err := gemini.NewFromEnv(gemini.Gemini_2_5_flash.String())
	if err != nil {
		log.Printf("gemini initialization warning: %v", err)
	}
	embedSDKClient, err := gemini.NewSDKClientFromEnv()
	if err != nil {
		panic("failed to initialize embeddings sdk client: " + err.Error())
	}

	dbURL := os.Getenv("DBURL")
	if dbURL == "" {
		panic("DBURL environment variable is empty")
	}
	database, err := db.Init(dbURL)
	if err != nil {
		panic("failed to initialize turso db: " + err.Error())
	}

	b, err := gotgbot.NewBot(token, nil)
	if err != nil {
		panic("failed to create new bot: " + err.Error())
	}

	ragService, err := rag.NewService(database, rag.NewGeminiEmbedder(embedSDKClient, os.Getenv("EMBEDDING_MODEL")))
	if err != nil {
		panic("failed to initialize rag service: " + err.Error())
	}

	_, _ = ragService.ReplaceIndex(context.Background(), []rag.InputDocument{
		{
			SourceID: "bootstrap-readme",
			Title:    "Takhtit Assistant Bootstrap",
			Content:  "RAG ingestion now uses LangChainGo primitives and connector-based sync for GitHub, Google Docs, and Google Calendar sources.",
			URL:      "local://bootstrap",
			Tags:     []string{"bootstrap", "rag", "langchaingo"},
		},
	})

	chatService := app.NewChatService(database, ragService, geminiClient)
	integrationService := app.NewIntegrationService(database)
	syncService := app.NewSyncService(database, integrationService, ragService)
	runMode := os.Getenv("RUN_MODE")
	startPeriodicSync(syncService)

	if port := os.Getenv("PORT"); port != "" {
		server := httpserver.New(syncService, integrationService)
		if runMode == "web" {
			log.Printf("http server listening on :%s in web mode", port)
			if err := http.ListenAndServe(":"+port, server.Handler()); err != nil {
				panic("failed to start http server: " + err.Error())
			}
			return
		}

		go func() {
			log.Printf("http server listening on :%s", port)
			if err := http.ListenAndServe(":"+port, server.Handler()); err != nil {
				log.Printf("http server stopped: %v", err)
			}
		}()
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})
	updater := ext.NewUpdater(dispatcher, nil)

	handlers := &telegram.HandlerSet{
		BotUserName:  botUserName,
		Chat:         chatService,
		Integrations: integrationService,
		Sync:         syncService,
	}
	handlers.Register(dispatcher)

	err = updater.StartPolling(b, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
			},
		},
	})

	if err != nil {
		panic("failed to start polling: " + err.Error())
	}

	updater.Idle()
}

func startPeriodicSync(syncService *app.SyncService) {
	intervalRaw := os.Getenv("SYNC_INTERVAL_MINUTES")
	if intervalRaw == "" {
		return
	}
	minutes, err := strconv.Atoi(intervalRaw)
	if err != nil || minutes <= 0 {
		log.Printf("invalid SYNC_INTERVAL_MINUTES=%q, skipping scheduler", intervalRaw)
		return
	}

	userRaw := os.Getenv("DEFAULT_SYNC_USER_ID")
	if userRaw == "" {
		log.Printf("DEFAULT_SYNC_USER_ID not set, skipping scheduler")
		return
	}
	userID, err := strconv.ParseInt(userRaw, 10, 64)
	if err != nil || userID <= 0 {
		log.Printf("invalid DEFAULT_SYNC_USER_ID=%q, skipping scheduler", userRaw)
		return
	}

	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			status := syncService.TriggerForUser(context.Background(), userID)
			log.Printf("scheduled sync user=%d status=%s", userID, status)
		}
	}()
}
