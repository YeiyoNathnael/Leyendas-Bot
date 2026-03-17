package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/YeiyoNathnael/ethchess-bot-tewdros/internal/app"
)

type HandlerSet struct {
	BotUserName  string
	Chat         *app.ChatService
	Integrations *app.IntegrationService
	Sync         *app.SyncService

	schedulerOnce sync.Once
}

func (h *HandlerSet) Register(dispatcher *ext.Dispatcher) {
	dispatcher.AddHandler(handlers.NewCommand("start", h.start))
	dispatcher.AddHandler(handlers.NewCommand("help", h.help))
	dispatcher.AddHandler(handlers.NewCommand("ask", h.ask))
	dispatcher.AddHandler(handlers.NewCommand("connect_github", h.connectGitHub))
	dispatcher.AddHandler(handlers.NewCommand("connect_google", h.connectGoogle))
	dispatcher.AddHandler(handlers.NewCommand("sync", h.syncNow))
	dispatcher.AddHandler(handlers.NewCommand("sync_status", h.syncStatus))
	dispatcher.AddHandler(handlers.NewMessage(h.anyMessage, h.observeAndHandleMessage))
}

func (h *HandlerSet) start(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	message := "Takhtit assistant is live. Tag me or reply to my message for instant help. If a question stays unanswered for 10 minutes, I will jump in automatically."
	_, err := ctx.EffectiveMessage.Reply(b, message, nil)
	return err
}

func (h *HandlerSet) help(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	helpText := strings.Join([]string{
		"Commands:",
		"Tag or reply to the bot for immediate answers",
		"Any unanswered question gets an automatic bot response after 10 minutes",
		"/ask <question> - Optional explicit ask command",
		"/connect_github - Connect your GitHub read access",
		"/connect_google - Connect Google Docs/Calendar read access",
		"/sync - Trigger immediate sync for your integrations",
		"/sync_status - Show latest sync result",
	}, "\n")
	_, err := ctx.EffectiveMessage.Reply(b, helpText, nil)
	return err
}

func (h *HandlerSet) ask(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	question := strings.TrimSpace(strings.TrimPrefix(ctx.EffectiveMessage.Text, "/ask"))
	return h.respondNow(b, ctx, question)
}

func (h *HandlerSet) connectGitHub(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	url := h.Integrations.GitHubConnect(ctx.EffectiveUser.Id)
	_, err := ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Connect GitHub read access: %s", url), nil)
	return err
}

func (h *HandlerSet) connectGoogle(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	url := h.Integrations.GoogleConnect(ctx.EffectiveUser.Id)
	_, err := ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Connect Google read access: %s", url), nil)
	return err
}

func (h *HandlerSet) syncNow(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	status := h.Sync.TriggerForUser(context.Background(), ctx.EffectiveUser.Id)
	_, err := ctx.EffectiveMessage.Reply(b, status, nil)
	return err
}

func (h *HandlerSet) syncStatus(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	status := h.Sync.StatusForUser(ctx.EffectiveUser.Id)
	_, err := ctx.EffectiveMessage.Reply(b, status, nil)
	return err
}

func (h *HandlerSet) anyMessage(msg *gotgbot.Message) bool {
	return msg != nil
}

func (h *HandlerSet) observeAndHandleMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	h.ensureScheduler(b)
	msg := ctx.EffectiveMessage
	if msg == nil {
		return nil
	}

	if msg.ReplyToMessage != nil {
		h.Chat.MarkPendingQuestionAnswered(context.Background(), msg.Chat.Id, int64(msg.ReplyToMessage.MessageId))
	}

	if isCommand(msg.Text) {
		return nil
	}

	if h.shouldHandleMessage(msg) {
		h.Chat.MarkPendingQuestionAnswered(context.Background(), msg.Chat.Id, int64(msg.MessageId))
		return h.respondNow(b, ctx, strings.TrimSpace(msg.Text))
	}

	if isQuestion(msg.Text) {
		h.enqueueQuestion(msg)
	}

	return nil
}

func (h *HandlerSet) shouldHandleMessage(msg *gotgbot.Message) bool {
	if msg == nil {
		return false
	}
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot {
		return true
	}
	for _, e := range msg.Entities {
		if e.Type == "mention" {
			mentioned := msg.Text[e.Offset : e.Offset+e.Length]
			if mentioned == h.BotUserName {
				return true
			}
		}
	}
	return false
}

func (h *HandlerSet) respondNow(b *gotgbot.Bot, ctx *ext.Context, question string) error {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil
	}
	h.Chat.RecordChatSession(context.Background(), ctx.EffectiveChat.Id, ctx.EffectiveUser.Id)
	answer, err := h.Chat.Answer(context.Background(), question)
	if err != nil {
		_, _ = ctx.EffectiveMessage.Reply(b, "I could not complete that request right now.", nil)
		return err
	}
	_, err = ctx.EffectiveMessage.Reply(b, answer, &gotgbot.SendMessageOpts{ParseMode: "MarkdownV2"})
	return err
}

func (h *HandlerSet) ensureScheduler(b *gotgbot.Bot) {
	h.schedulerOnce.Do(func() {
		ticker := time.NewTicker(1 * time.Minute)
		go func() {
			defer ticker.Stop()
			for range ticker.C {
				h.dispatchDueQuestions(b)
			}
		}()
	})
}

func (h *HandlerSet) enqueueQuestion(msg *gotgbot.Message) {
	if msg == nil || strings.TrimSpace(msg.Text) == "" || msg.From == nil {
		return
	}
	h.Chat.EnqueuePendingQuestion(context.Background(), msg.Chat.Id, int64(msg.MessageId), msg.From.Id, strings.TrimSpace(msg.Text))
}

func (h *HandlerSet) dispatchDueQuestions(b *gotgbot.Bot) {
	due := h.Chat.ListDuePendingQuestions(context.Background(), 10*time.Minute, 50)
	for _, q := range due {
		h.Chat.RecordChatSession(context.Background(), q.ChatID, q.UserID)
		answer, err := h.Chat.Answer(context.Background(), q.Question)
		if err != nil {
			continue
		}
		_, _ = b.SendMessage(q.ChatID, answer, &gotgbot.SendMessageOpts{ParseMode: "MarkdownV2"})
		h.Chat.MarkPendingQuestionDispatched(context.Background(), q.ID)
	}
}

func isCommand(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/")
}

func isQuestion(text string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "?") {
		return true
	}
	prefixes := []string{"who ", "what ", "when ", "where ", "why ", "how ", "can ", "could ", "would ", "should ", "is ", "are ", "do ", "does ", "did "}
	for _, p := range prefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}
