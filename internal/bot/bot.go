package bot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"

	"leyendas-bot/internal/ai"
	"leyendas-bot/internal/config"
	"leyendas-bot/internal/db"
)

// BotEngine wraps the Telegram bot, dispatcher, updater, and application services.
type BotEngine struct {
	bot        *gotgbot.Bot
	updater    *ext.Updater
	dispatcher *ext.Dispatcher
	cfg        *config.Config
	store      *db.Store
	executor   *ai.Executor
	aiAgent    *ai.Agent
}

// NewBotEngine initializes the Telegram bot engine and registers all handlers.
func NewBotEngine(
	cfg *config.Config,
	store *db.Store,
	executor *ai.Executor,
	aiAgent *ai.Agent,
) (*BotEngine, error) {
	b, err := gotgbot.NewBot(cfg.TelegramBotToken, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Timeout: 30 * time.Second,
			},
		},
		DisableTokenCheck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gotgbot: %w", err)
	}

	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Printf("[ERROR] Update %d failed: %v", ctx.Update.UpdateId, err)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	engine := &BotEngine{
		bot:        b,
		updater:    ext.NewUpdater(dispatcher, nil),
		dispatcher: dispatcher,
		cfg:        cfg,
		store:      store,
		executor:   executor,
		aiAgent:    aiAgent,
	}

	engine.registerHandlers()
	return engine, nil
}

// Bot returns the underlying *gotgbot.Bot.
func (e *BotEngine) Bot() *gotgbot.Bot {
	return e.bot
}

// Start begins receiving Telegram updates via long polling.
func (e *BotEngine) Start() error {
	err := e.updater.StartPolling(e.bot, &ext.PollingOpts{
		DropPendingUpdates: false,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 20,
			AllowedUpdates: []string{
				"message",
				"edited_message",
				"callback_query",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start polling: %w", err)
	}

	log.Printf("🤖 Telegram Bot started as @%s", e.bot.Username)
	return nil
}

// Stop gracefully stops receiving updates and halts the dispatcher.
func (e *BotEngine) Stop() error {
	return e.updater.Stop()
}

func (e *BotEngine) registerHandlers() {
	// --- 1. Slash Commands ---
	e.dispatcher.AddHandler(handlers.NewCommand("start", e.handleStart))
	e.dispatcher.AddHandler(handlers.NewCommand("help", e.handleHelp))
	e.dispatcher.AddHandler(handlers.NewCommand("register", e.handleRegister))
	e.dispatcher.AddHandler(handlers.NewCommand("whoami", e.handleWhoAmI))
	e.dispatcher.AddHandler(handlers.NewCommand("tasks", e.handleTasks))
	e.dispatcher.AddHandler(handlers.NewCommand("mytasks", e.handleMyTasks))
	e.dispatcher.AddHandler(handlers.NewCommand("task", e.handleTaskDetails))
	e.dispatcher.AddHandler(handlers.NewCommand("done", e.handleMarkDone))
	e.dispatcher.AddHandler(handlers.NewCommand("schedule", e.handleSchedule))
	e.dispatcher.AddHandler(handlers.NewCommand("standup", e.handleStandup))
	e.dispatcher.AddHandler(handlers.NewCommand("agenda", e.handleAgenda))
	e.dispatcher.AddHandler(handlers.NewCommand("meetings", e.handleAgenda))
	e.dispatcher.AddHandler(handlers.NewCommand("status", e.handleStatus))

	// --- 2. Thread Reply Handler (Standup Submissions in Groups) ---
	e.dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			if msg == nil {
				return false
			}
			isGroup := msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"
			return isGroup && msg.ReplyToMessage != nil && msg.Text != "" && !message.Command(msg)
		},
		e.handleStandupReply,
	))

	// --- 3. Natural Language AI Router: Group Mentions ---
	e.dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			if msg == nil {
				return false
			}
			isGroup := msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"
			return isGroup && msg.Text != "" && !message.Command(msg) && e.botMentionFilter(msg)
		},
		e.handleAIMessage,
	))

	// --- 4. Natural Language AI Router: Direct Messages (DMs) ---
	e.dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			if msg == nil {
				return false
			}
			return msg.Chat.Type == "private" && msg.Text != "" && !message.Command(msg)
		},
		e.handleAIMessage,
	))
}

func (e *BotEngine) resolveSender(ctx *ext.Context) (*db.TeamMember, error) {
	if ctx.EffectiveUser == nil {
		return nil, nil
	}
	return e.store.GetMemberByTelegramID(context.Background(), ctx.EffectiveUser.Id)
}

func (e *BotEngine) botMentionFilter(msg *gotgbot.Message) bool {
	if msg == nil || msg.Text == "" {
		return false
	}
	botUsername := "@" + strings.ToLower(e.bot.Username)
	for _, ent := range msg.Entities {
		if ent.Type == "mention" {
			mentionText := strings.ToLower(msg.Text[ent.Offset : ent.Offset+ent.Length])
			if mentionText == botUsername {
				return true
			}
		}
	}
	return strings.Contains(strings.ToLower(msg.Text), botUsername)
}
