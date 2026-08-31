package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"leyendas-bot/internal/db"
)

func (e *BotEngine) handleStart(b *gotgbot.Bot, ctx *ext.Context) error {
	name := ctx.EffectiveUser.FirstName
	text := fmt.Sprintf("👋 Welcome <b>%s</b> to <b>Leyendas Bot</b>!\n\n"+
		"I automate standups, synchronize Notion tasks, schedule Google Calendar/Meet calls, and support natural-language commands powered by Gemma 4 31B.\n\n"+
		"👉 If you haven't registered yet, send: <code>/register &lt;your-google-email&gt; [role]</code> in DM.\n\n"+
		"Use /help to see all available commands.", name)

	_, err := ctx.EffectiveMessage.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleHelp(b *gotgbot.Bot, ctx *ext.Context) error {
	helpText := `📖 <b>Leyendas Bot Commands</b>

<b>Identity & Registration:</b>
• /register &lt;google-email&gt; [role] — Register your profile (DM only)
• /whoami — View your resolved identity details

<b>Task Management (Notion):</b>
• /tasks — List all open workspace tasks
• /mytasks — DM your personal open tasks
• /task &lt;name/id&gt; — Look up details of a specific task
• /done &lt;name/id&gt; — Mark a task as Done

<b>Meetings & Standups:</b>
• /schedule &lt;datetime&gt; [duration_mins] — Schedule Google Calendar/Meet + Notion row
• /standup — Check or trigger today's standup round
• /agenda or /meetings — View upcoming meetings & Google Doc links

<b>General:</b>
• /status — Quick workspace summary (tasks, next deadline, next meeting)
• /help — Show this help message

💬 <i>You can also mention @bot or DM me in plain English! (e.g. "@bot I'll take the login page redesign, due Friday")</i>`

	_, err := ctx.EffectiveMessage.Reply(b, helpText, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleRegister(b *gotgbot.Bot, ctx *ext.Context) error {
	if ctx.EffectiveChat.Type != "private" {
		_, err := ctx.EffectiveMessage.Reply(b, "🔒 For privacy, please run <code>/register</code> in a direct message with the bot.", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	args := strings.Fields(ctx.EffectiveMessage.Text)
	if len(args) < 2 {
		text := "ℹ️ <b>Usage:</b> <code>/register &lt;your-google-email&gt; [role]</code>\n\nExample:\n<code>/register nathnael@gmail.com Frontend Lead</code>"
		_, err := ctx.EffectiveMessage.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	email := args[1]
	role := "Team Member"
	if len(args) > 2 {
		role = strings.Join(args[2:], " ")
	}

	fullName := ctx.EffectiveUser.FirstName
	if ctx.EffectiveUser.LastName != "" {
		fullName += " " + ctx.EffectiveUser.LastName
	}

	member := &db.TeamMember{
		FullName:         fullName,
		TelegramUserID:   ctx.EffectiveUser.Id,
		TelegramUsername: ctx.EffectiveUser.Username,
	}

	res, err := e.executor.ExecuteTool(context.Background(), member, "register_member", map[string]any{
		"google_email": email,
		"role_module":  role,
	})
	if err != nil {
		_, _ = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("❌ Registration failed: %v", err), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	_, err = ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleWhoAmI(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "get_my_identity", nil)
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleTasks(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "list_open_tasks", nil)
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleMyTasks(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	if member == nil {
		_, err := ctx.EffectiveMessage.Reply(b, "❌ You are not registered yet. Run <code>/register &lt;google-email&gt;</code> in DM.", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	res, _ := e.executor.ExecuteTool(context.Background(), member, "list_my_tasks", nil)

	if ctx.EffectiveChat.Type != "private" {
		// DM the requester to keep group clean
		_, err := b.SendMessage(member.TelegramUserID, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		if err != nil {
			_, err = ctx.EffectiveMessage.Reply(b, "⚠️ Could not DM you. Please start a private chat with me first.", nil)
			return err
		}
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("📩 Sent your open tasks to your direct messages, <b>%s</b>.", member.FullName), &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleTaskDetails(b *gotgbot.Bot, ctx *ext.Context) error {
	args := strings.TrimSpace(strings.TrimPrefix(ctx.EffectiveMessage.Text, "/task"))
	if args == "" {
		_, err := ctx.EffectiveMessage.Reply(b, "ℹ️ <b>Usage:</b> <code>/task &lt;task-name-or-id&gt;</code>", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "get_task_details", map[string]any{
		"task_name_or_id": args,
	})
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleMarkDone(b *gotgbot.Bot, ctx *ext.Context) error {
	args := strings.TrimSpace(strings.TrimPrefix(ctx.EffectiveMessage.Text, "/done"))
	if args == "" {
		_, err := ctx.EffectiveMessage.Reply(b, "ℹ️ <b>Usage:</b> <code>/done &lt;task-name-or-id&gt;</code>", &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "mark_task_done", map[string]any{
		"task_name_or_id": args,
	})
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleSchedule(b *gotgbot.Bot, ctx *ext.Context) error {
	args := strings.TrimSpace(strings.TrimPrefix(ctx.EffectiveMessage.Text, "/schedule"))
	if args == "" {
		text := "ℹ️ <b>Usage:</b> <code>/schedule &lt;ISO-8601-datetime&gt; [duration_mins] [title]</code>\n\nExample:\n<code>/schedule 2026-09-02T15:00:00Z 45 Sprint Planning</code>"
		_, err := ctx.EffectiveMessage.Reply(b, text, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
		return err
	}

	parts := strings.Fields(args)
	startDatetime := parts[0]
	durationMins := 30
	title := "Team Sync"

	if len(parts) > 1 {
		var d int
		if _, err := fmt.Sscanf(parts[1], "%d", &d); err == nil && d > 0 {
			durationMins = d
			if len(parts) > 2 {
				title = strings.Join(parts[2:], " ")
			}
		} else {
			title = strings.Join(parts[1:], " ")
		}
	}

	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "schedule_meeting", map[string]any{
		"start_datetime":   startDatetime,
		"duration_minutes": float64(durationMins),
		"meeting_name":     title,
		"category":         "Planning",
	})
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleStandup(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "trigger_standup_round", nil)
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleAgenda(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "get_upcoming_meetings", nil)
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}

func (e *BotEngine) handleStatus(b *gotgbot.Bot, ctx *ext.Context) error {
	member, _ := e.resolveSender(ctx)
	res, _ := e.executor.ExecuteTool(context.Background(), member, "get_workspace_status", nil)
	_, err := ctx.EffectiveMessage.Reply(b, res.Message, &gotgbot.SendMessageOpts{ParseMode: "HTML"})
	return err
}
