package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"leyendas-bot/internal/db"
)

func (s *Scheduler) checkDeadlines() {
	if s.cfg.NotionTasksDBID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tasks, err := s.notionClient.QueryOpenTasks(ctx, s.cfg.NotionTasksDBID)
	if err != nil {
		log.Printf("[ERROR] Failed to query open tasks for deadline check: %v", err)
		return
	}

	now := time.Now().UTC()

	for _, task := range tasks {
		if task.DueDate == nil {
			continue
		}

		due := task.DueDate.UTC()
		diff := due.Sub(now)

		var member *db.TeamMember
		if task.AssigneePageID != "" {
			member, _ = s.store.GetMemberByNotionPageID(ctx, task.AssigneePageID)
		}

		// 1. 48-Hour Reminder (Between 24h and 48h before due date)
		if diff > 24*time.Hour && diff <= 48*time.Hour {
			s.dispatchReminder(ctx, task.ID, "48h", member,
				fmt.Sprintf("⏰ <b>Deadline Reminder (48h)</b>\n\nTask '<b>%s</b>' is due in ~48 hours (%s).", task.TaskName, task.DueDateStr),
				false)
		}

		// 2. 24-Hour Reminder (Between 0 and 24h before due date)
		if diff > 0 && diff <= 24*time.Hour {
			s.dispatchReminder(ctx, task.ID, "24h", member,
				fmt.Sprintf("⏰ <b>Urgent Deadline Reminder (24h)</b>\n\nTask '<b>%s</b>' is due tomorrow (%s).", task.TaskName, task.DueDateStr),
				false)
		}

		// 3. Due Date Reminder (Due today or slightly past 0-24h)
		if diff <= 0 && diff > -24*time.Hour {
			s.dispatchReminder(ctx, task.ID, "due_today", member,
				fmt.Sprintf("🚨 <b>Task Due Today</b>\n\nTask '<b>%s</b>' is scheduled for completion today (%s).", task.TaskName, task.DueDateStr),
				false)
		}

		// 4. Overdue Escalation (>24 hours past due date) -> Ping the Group!
		if diff <= -24*time.Hour {
			assigneeName := "Unassigned"
			if member != nil {
				assigneeName = fmt.Sprintf("<b>%s</b> (@%s)", member.FullName, member.TelegramUsername)
			}
			escalationMsg := fmt.Sprintf("⚠️ <b>Overdue Task Escalation</b>\n\nTask '<b>%s</b>' is overdue by more than 24 hours!\n• Assignee: %s\n• Due Date: %s\n\nPlease check in on task progress.",
				task.TaskName, assigneeName, task.DueDateStr)

			s.dispatchReminder(ctx, task.ID, "overdue_24h", member, escalationMsg, true)
		}
	}
}

func (s *Scheduler) dispatchReminder(ctx context.Context, taskID, reminderType string, member *db.TeamMember, text string, escalateToGroup bool) {
	// Atomic check in Turso so reminder is NEVER sent twice
	ok, err := s.store.TryRecordReminder(ctx, taskID, reminderType)
	if err != nil {
		log.Printf("[ERROR] Failed to record reminder lock: %v", err)
		return
	}
	if !ok {
		return // Already sent previously
	}

	// If group escalation
	if escalateToGroup && s.cfg.TelegramGroupChatID != 0 {
		_, err := s.bot.SendMessage(s.cfg.TelegramGroupChatID, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		if err != nil {
			log.Printf("[ERROR] Failed to post escalation reminder to group: %v", err)
		}
		return
	}

	// Otherwise send DM to assignee if resolved
	if member != nil && member.TelegramUserID != 0 {
		_, err := s.bot.SendMessage(member.TelegramUserID, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		if err != nil {
			log.Printf("[WARN] Failed to DM member %d reminder: %v", member.TelegramUserID, err)
		}
	}
}
