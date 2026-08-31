package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
)

func (e *BotEngine) handleStandupReply(b *gotgbot.Bot, ctx *ext.Context) error {
	repliedMsg := ctx.EffectiveMessage.ReplyToMessage
	if repliedMsg == nil {
		return nil
	}

	today := time.Now().Format("2006-01-02")
	round, err := e.store.GetActiveStandupRound(context.Background(), today)
	if err != nil || round == nil || round.IsClosed {
		return nil
	}

	// Check if replying to the standup prompt
	if repliedMsg.MessageId != round.PromptMessageID {
		return nil
	}

	sender, _ := e.resolveSender(ctx)
	fullName := ctx.EffectiveUser.FirstName
	if sender != nil {
		fullName = sender.FullName
	}

	err = e.store.RecordStandupReply(
		context.Background(),
		round.ID,
		ctx.EffectiveUser.Id,
		fullName,
		ctx.EffectiveMessage.Text,
	)
	if err != nil {
		return fmt.Errorf("failed to save standup reply: %w", err)
	}

	confirmText := fmt.Sprintf("✅ Standup recorded for <b>%s</b>!", fullName)
	_, err = ctx.EffectiveMessage.Reply(b, confirmText, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}
