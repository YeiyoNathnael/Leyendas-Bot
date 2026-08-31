package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"leyendas-bot/internal/db"
)

func (s *Scheduler) pollNotionTasks() {
	if s.cfg.NotionTasksDBID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tasks, err := s.notionClient.QueryAllTasks(ctx, s.cfg.NotionTasksDBID)
	if err != nil {
		log.Printf("[ERROR] Failed to query Notion tasks for change polling: %v", err)
		return
	}

	for _, task := range tasks {
		snap, err := s.store.GetTaskSnapshot(ctx, task.ID)
		if err != nil {
			log.Printf("[WARN] Failed to fetch task snapshot for %s: %v", task.ID, err)
			continue
		}

		lastEditedStr := task.LastEditedTime.Format(time.RFC3339)

		// 1. Initial Discovery / Seeding
		if snap == nil {
			isDone := task.Status == "Done"
			newSnap := &db.TaskSnapshot{
				TaskID:          task.ID,
				LastStatus:      task.Status,
				LastEditedTime:  lastEditedStr,
				NotifiedDone:    isDone, // Do not spam old completed tasks
				NotifiedCreated: true,   // Mark seeded to avoid spamming historical tasks
			}
			_ = s.store.UpsertTaskSnapshot(ctx, newSnap)
			continue
		}

		var member *db.TeamMember
		if task.AssigneePageID != "" {
			member, _ = s.store.GetMemberByNotionPageID(ctx, task.AssigneePageID)
		}
		assigneeName := "Unassigned"
		if member != nil {
			assigneeName = member.FullName
		}

		// 2. Status Changed to "Done" Alert (Celebratory ping)
		if task.Status == "Done" && !snap.NotifiedDone {
			if s.cfg.TelegramGroupChatID != 0 {
				doneMsg := fmt.Sprintf("🎉 <b>Task Completed!</b>\n\n'<b>%s</b>' has been marked as <b>Done</b>!\n👏 Great job, <b>%s</b>!",
					task.TaskName, assigneeName)
				_, err := s.bot.SendMessage(s.cfg.TelegramGroupChatID, doneMsg, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
				if err != nil {
					log.Printf("[ERROR] Failed to send task done celebratory message: %v", err)
				}
			}

			snap.NotifiedDone = true
			snap.LastStatus = "Done"
			snap.LastEditedTime = lastEditedStr
			_ = s.store.UpsertTaskSnapshot(ctx, snap)
			continue
		}

		// 3. Task Reopened (Was Done, now moved back to In Progress / Not started)
		if task.Status != "Done" && snap.NotifiedDone {
			snap.NotifiedDone = false
			snap.LastStatus = task.Status
			snap.LastEditedTime = lastEditedStr
			_ = s.store.UpsertTaskSnapshot(ctx, snap)
			continue
		}

		// 4. Newly Created Task Alert (after initial baseline)
		if !snap.NotifiedCreated && task.AssigneePageID != "" {
			// DM Assignee
			if member != nil && member.TelegramUserID != 0 {
				dmText := fmt.Sprintf("📋 <b>New Task Assigned</b>\n\nYou've been assigned: '<b>%s</b>'.\n• Due: %s\n• Priority: %s",
					task.TaskName, task.DueDateStr, task.Priority)
				_, _ = s.bot.SendMessage(member.TelegramUserID, dmText, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
			}

			// Announce to Group
			if s.cfg.TelegramGroupChatID != 0 {
				groupAnnounce := fmt.Sprintf("📌 <b>New Task:</b> '<b>%s</b>' assigned to <b>%s</b>.", task.TaskName, assigneeName)
				_, _ = s.bot.SendMessage(s.cfg.TelegramGroupChatID, groupAnnounce, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
			}

			snap.NotifiedCreated = true
			snap.LastStatus = task.Status
			snap.LastEditedTime = lastEditedStr
			_ = s.store.UpsertTaskSnapshot(ctx, snap)
			continue
		}

		// Update snapshot state
		if snap.LastStatus != task.Status || snap.LastEditedTime != lastEditedStr {
			snap.LastStatus = task.Status
			snap.LastEditedTime = lastEditedStr
			_ = s.store.UpsertTaskSnapshot(ctx, snap)
		}
	}
}
