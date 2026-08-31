package scheduler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

func (s *Scheduler) runStandupPrompt() {
	if s.cfg.TelegramGroupChatID == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	today := time.Now().Format("2006-01-02")
	promptText := fmt.Sprintf("🌅 <b>Daily Standup — %s</b>\n\n"+
		"Good morning team! Please reply directly to this message with your updates:\n"+
		"1. <b>What did you complete yesterday?</b>\n"+
		"2. <b>What will you work on today?</b>\n"+
		"3. <b>Any blockers or help needed?</b>", today)

	msg, err := s.bot.SendMessage(s.cfg.TelegramGroupChatID, promptText, &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
	})
	if err != nil {
		log.Printf("[ERROR] Failed to post standup prompt: %v", err)
		return
	}

	_, err = s.store.GetOrCreateTodayStandupRound(ctx, today, msg.MessageId)
	if err != nil {
		log.Printf("[ERROR] Failed to record standup round in DB: %v", err)
	}
}

func (s *Scheduler) compileStandupDigest() {
	if s.cfg.TelegramGroupChatID == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	today := time.Now().Format("2006-01-02")
	round, err := s.store.GetActiveStandupRound(ctx, today)
	if err != nil || round == nil || round.IsClosed {
		return
	}

	replies, err := s.store.GetStandupRepliesForRound(ctx, round.ID)
	if err != nil {
		log.Printf("[ERROR] Failed to get standup replies: %v", err)
		return
	}

	members, _ := s.store.ListMembers(ctx)
	respondedUserIDs := make(map[int64]bool)
	for _, r := range replies {
		respondedUserIDs[r.TelegramUserID] = true
	}

	// 1. Send gentle DM nudge to members who haven't replied
	for _, m := range members {
		if !respondedUserIDs[m.TelegramUserID] {
			nudgeText := fmt.Sprintf("👋 Gentle reminder, <b>%s</b>: please reply to today's standup thread in the group when you get a chance!", m.FullName)
			_, _ = s.bot.SendMessage(m.TelegramUserID, nudgeText, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		}
	}

	// 2. Compile summary digest
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 <b>Standup Summary Digest — %s</b>\n\n", today))

	if len(replies) == 0 {
		sb.WriteString("<i>No standup updates submitted for today yet.</i>")
	} else {
		for i, r := range replies {
			sb.WriteString(fmt.Sprintf("👤 <b>%s:</b>\n%s\n\n", r.FullName, r.ReplyText))
			_ = i
		}
	}

	var missingNames []string
	for _, m := range members {
		if !respondedUserIDs[m.TelegramUserID] {
			missingNames = append(missingNames, m.FullName)
		}
	}
	if len(missingNames) > 0 {
		sb.WriteString(fmt.Sprintf("⏳ <b>Pending updates from:</b> %s", strings.Join(missingNames, ", ")))
	}

	digestMsg, err := s.bot.SendMessage(s.cfg.TelegramGroupChatID, sb.String(), &gotgbot.SendMessageOpts{
		ParseMode: "HTML",
	})
	if err != nil {
		log.Printf("[ERROR] Failed to post standup digest: %v", err)
		return
	}

	_ = s.store.CloseStandupRound(ctx, round.ID, digestMsg.MessageId)
}
