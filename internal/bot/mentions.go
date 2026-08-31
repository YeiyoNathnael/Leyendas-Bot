package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func (e *BotEngine) handleAIMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	sender, err := e.resolveSender(ctx)
	if err != nil {
		_, _ = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("⚠️ Database error: %v", err), nil)
		return err
	}

	// Identity resolution check before model sees anything (per Section 6.1 spec)
	if sender == nil {
		replyText := "🔒 <b>Unregistered Identity</b>\n\nBefore I can process your request or assign tasks to you, please register by sending:\n<code>/register &lt;your-google-email&gt; [role]</code>\nin a direct message to me."
		_, err := ctx.EffectiveMessage.Reply(b, replyText, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	userMsg := ctx.EffectiveMessage.Text
	// Strip bot mention tag if in group chat
	if e.bot.Username != "" {
		userMsg = strings.ReplaceAll(userMsg, "@"+e.bot.Username, "")
		userMsg = strings.ReplaceAll(userMsg, "@"+strings.ToLower(e.bot.Username), "")
	}
	userMsg = strings.TrimSpace(userMsg)

	if userMsg == "" {
		_, err := ctx.EffectiveMessage.Reply(b, fmt.Sprintf("👋 How can I help you today, <b>%s</b>?", sender.FullName), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	// Send typing action
	_, _ = b.SendChatAction(ctx.EffectiveChat.Id, "typing", nil)

	reqCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	aiReply, err := e.aiAgent.ProcessNaturalLanguageMessage(reqCtx, sender, userMsg)
	if err != nil {
		_, _ = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("❌ Error processing request: %v", err), nil)
		return err
	}

	_, err = ctx.EffectiveMessage.Reply(b, aiReply, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}
