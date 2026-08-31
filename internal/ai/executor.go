package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"leyendas-bot/internal/calendar"
	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
)

// Executor coordinates tool execution against Notion, Google Calendar, and Turso DB.
type Executor struct {
	store        *db.Store
	notionClient *notion.Client
	calService   *calendar.Service
	tasksDBID    string
	teamDBID     string
	meetingsDBID string
}

// NewExecutor creates a new Executor instance.
func NewExecutor(
	store *db.Store,
	notionClient *notion.Client,
	calService *calendar.Service,
	tasksDBID, teamDBID, meetingsDBID string,
) *Executor {
	return &Executor{
		store:        store,
		notionClient: notionClient,
		calService:   calService,
		tasksDBID:    tasksDBID,
		teamDBID:     teamDBID,
		meetingsDBID: meetingsDBID,
	}
}

// ToolResult encapsulates the outcome of a tool execution.
type ToolResult struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// ExecuteTool safely dispatches and runs a tool call with sender context.
func (e *Executor) ExecuteTool(ctx context.Context, sender *db.TeamMember, toolName string, args map[string]any) (*ToolResult, error) {
	switch toolName {
	case "create_task":
		return e.execCreateTask(ctx, sender, args)

	case "mark_task_done":
		return e.execMarkTaskDone(ctx, sender, args)

	case "list_open_tasks":
		return e.execListOpenTasks(ctx, sender, args)

	case "list_my_tasks":
		return e.execListMyTasks(ctx, sender)

	case "get_task_details":
		return e.execGetTaskDetails(ctx, args)

	case "schedule_meeting":
		return e.execScheduleMeeting(ctx, sender, args)

	case "get_upcoming_meetings":
		return e.execGetUpcomingMeetings(ctx)

	case "get_workspace_status":
		return e.execGetWorkspaceStatus(ctx)

	case "get_my_identity":
		return e.execGetMyIdentity(sender)

	case "register_member":
		return e.execRegisterMember(ctx, sender, args)

	case "trigger_standup_round":
		return e.execTriggerStandupRound(ctx)

	default:
		return &ToolResult{
			Success: false,
			Message: fmt.Sprintf("Unknown tool '%s'", toolName),
		}, nil
	}
}

func (e *Executor) execCreateTask(ctx context.Context, sender *db.TeamMember, args map[string]any) (*ToolResult, error) {
	taskName, _ := args["task_name"].(string)
	if strings.TrimSpace(taskName) == "" {
		return &ToolResult{Success: false, Message: "Task name cannot be empty."}, nil
	}

	assigneePageID, _ := args["assignee_notion_page_id"].(string)
	if assigneePageID == "" && sender != nil {
		assigneePageID = sender.NotionPageID
	}

	taskType, _ := args["task_type"].(string)
	dueDate, _ := args["due_date"].(string)
	priority, _ := args["priority"].(string)
	description, _ := args["description"].(string)

	created, err := e.notionClient.CreateTask(ctx, e.tasksDBID, notion.CreateTaskParams{
		TaskName:             taskName,
		AssigneeNotionPageID: assigneePageID,
		TaskType:             taskType,
		DueDate:              dueDate,
		Priority:             priority,
		Description:          description,
	})
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to create task in Notion: %v", err)}, nil
	}

	// Record snapshot in DB so initial creation isn't flagged again by polling
	_ = e.store.UpsertTaskSnapshot(ctx, &db.TaskSnapshot{
		TaskID:          created.ID,
		LastStatus:      "Not started",
		LastEditedTime:  time.Now().UTC().Format(time.RFC3339),
		NotifiedDone:    false,
		NotifiedCreated: true,
	})

	assigneeName := "Unassigned"
	if sender != nil && assigneePageID == sender.NotionPageID {
		assigneeName = sender.FullName
	}

	msg := fmt.Sprintf("✅ Task created: <b>%s</b>\n• Assignee: %s\n• Status: Not started", created.TaskName, assigneeName)
	if created.DueDateStr != "" {
		msg += fmt.Sprintf("\n• Due Date: %s", created.DueDateStr)
	}
	if created.Priority != "" {
		msg += fmt.Sprintf("\n• Priority: %s", created.Priority)
	}

	return &ToolResult{
		Success: true,
		Message: msg,
		Data: map[string]any{
			"task_id":   created.ID,
			"task_name": created.TaskName,
			"url":       created.URL,
		},
	}, nil
}

func (e *Executor) execMarkTaskDone(ctx context.Context, sender *db.TeamMember, args map[string]any) (*ToolResult, error) {
	taskNameOrID, _ := args["task_name_or_id"].(string)
	if strings.TrimSpace(taskNameOrID) == "" {
		return &ToolResult{Success: false, Message: "Please specify the task name or ID."}, nil
	}

	task, err := e.notionClient.FindTaskByNameOrID(ctx, e.tasksDBID, taskNameOrID)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	updated, err := e.notionClient.MarkTaskDone(ctx, task.ID)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to mark task as Done: %v", err)}, nil
	}

	// Update Turso snapshot
	_ = e.store.UpsertTaskSnapshot(ctx, &db.TaskSnapshot{
		TaskID:          updated.ID,
		LastStatus:      "Done",
		LastEditedTime:  time.Now().UTC().Format(time.RFC3339),
		NotifiedDone:    true,
		NotifiedCreated: true,
	})

	msg := fmt.Sprintf("🎉 Task <b>%s</b> has been marked as <b>Done</b>!", updated.TaskName)
	return &ToolResult{
		Success: true,
		Message: msg,
		Data: map[string]any{
			"task_id":   updated.ID,
			"task_name": updated.TaskName,
			"status":    "Done",
		},
	}, nil
}

func (e *Executor) execListOpenTasks(ctx context.Context, sender *db.TeamMember, args map[string]any) (*ToolResult, error) {
	tasks, err := e.notionClient.QueryOpenTasks(ctx, e.tasksDBID)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to query tasks: %v", err)}, nil
	}

	if len(tasks) == 0 {
		return &ToolResult{Success: true, Message: "🌴 No open tasks found! All clear."}, nil
	}

	members, _ := e.store.ListMembers(ctx)
	memberMap := make(map[string]string)
	for _, m := range members {
		memberMap[m.NotionPageID] = m.FullName
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 <b>Open Tasks (%d)</b>:\n\n", len(tasks)))

	for i, t := range tasks {
		assignee := "Unassigned"
		if name, ok := memberMap[t.AssigneePageID]; ok {
			assignee = name
		}
		due := "No deadline"
		if t.DueDateStr != "" {
			due = t.DueDateStr
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   👤 Assignee: %s | 📅 Due: %s | 🏷️ Status: %s\n",
			i+1, t.TaskName, assignee, due, t.Status))
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (e *Executor) execListMyTasks(ctx context.Context, sender *db.TeamMember) (*ToolResult, error) {
	if sender == nil {
		return &ToolResult{Success: false, Message: "Identity unresolved. Please run /register first."}, nil
	}

	tasks, err := e.notionClient.QueryMyOpenTasks(ctx, e.tasksDBID, sender.NotionPageID)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to query your tasks: %v", err)}, nil
	}

	if len(tasks) == 0 {
		return &ToolResult{Success: true, Message: fmt.Sprintf("🎉 You have no open tasks, <b>%s</b>!", sender.FullName)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 <b>Open Tasks for %s (%d)</b>:\n\n", sender.FullName, len(tasks)))

	for i, t := range tasks {
		due := "No deadline"
		if t.DueDateStr != "" {
			due = t.DueDateStr
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   📅 Due: %s | ⚡ Priority: %s | 🏷️ %s\n",
			i+1, t.TaskName, due, t.Priority, t.Status))
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (e *Executor) execGetTaskDetails(ctx context.Context, args map[string]any) (*ToolResult, error) {
	query, _ := args["task_name_or_id"].(string)
	if strings.TrimSpace(query) == "" {
		return &ToolResult{Success: false, Message: "Please specify a task name or ID."}, nil
	}

	task, err := e.notionClient.FindTaskByNameOrID(ctx, e.tasksDBID, query)
	if err != nil {
		return &ToolResult{Success: false, Message: err.Error()}, nil
	}

	var assigneeName string
	if task.AssigneePageID != "" {
		if m, _ := e.store.GetMemberByNotionPageID(ctx, task.AssigneePageID); m != nil {
			assigneeName = m.FullName
		}
	}
	if assigneeName == "" {
		assigneeName = "Unassigned"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📌 <b>Task Details: %s</b>\n", task.TaskName))
	sb.WriteString(fmt.Sprintf("• Status: %s\n", task.Status))
	sb.WriteString(fmt.Sprintf("• Assignee: %s\n", assigneeName))
	if task.Priority != "" {
		sb.WriteString(fmt.Sprintf("• Priority: %s\n", task.Priority))
	}
	if task.DueDateStr != "" {
		sb.WriteString(fmt.Sprintf("• Due Date: %s\n", task.DueDateStr))
	}
	if task.TaskType != "" {
		sb.WriteString(fmt.Sprintf("• Type: %s\n", task.TaskType))
	}
	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("• Description: %s\n", task.Description))
	}

	return &ToolResult{
		Success: true,
		Message: sb.String(),
		Data: map[string]any{
			"task_id":   task.ID,
			"task_name": task.TaskName,
			"status":    task.Status,
		},
	}, nil
}

func (e *Executor) execScheduleMeeting(ctx context.Context, sender *db.TeamMember, args map[string]any) (*ToolResult, error) {
	startStr, _ := args["start_datetime"].(string)
	if startStr == "" {
		return &ToolResult{Success: false, Message: "Please provide a valid start datetime."}, nil
	}

	var startTime time.Time
	var err error
	if startTime, err = time.Parse(time.RFC3339, startStr); err != nil {
		if startTime, err = time.Parse("2006-01-02 15:04", startStr); err != nil {
			return &ToolResult{Success: false, Message: "Invalid date format. Use ISO-8601 e.g. 2026-09-02T15:00:00Z"}, nil
		}
	}

	durationMin := 30
	if d, ok := args["duration_minutes"].(float64); ok && d > 0 {
		durationMin = int(d)
	} else if d, ok := args["duration_minutes"].(int); ok && d > 0 {
		durationMin = d
	}

	endTime := startTime.Add(time.Duration(durationMin) * time.Minute)

	meetingName, _ := args["meeting_name"].(string)
	if meetingName == "" {
		meetingName = "Team Sync / Planning"
	}
	category, _ := args["category"].(string)
	if category == "" {
		category = "Planning"
	}

	// Resolve all registered members' Google Emails and Notion Page IDs
	members, _ := e.store.ListMembers(ctx)
	var emails []string
	var attendeePageIDs []string

	for _, m := range members {
		if m.GoogleEmail != "" {
			emails = append(emails, m.GoogleEmail)
		}
		if m.NotionPageID != "" {
			attendeePageIDs = append(attendeePageIDs, m.NotionPageID)
		}
	}

	var meetLink string
	var htmlURL string

	if e.calService != nil {
		calRes, err := e.calService.CreateEventWithMeet(ctx, "primary", calendar.CreateMeetingParams{
			Summary:     meetingName,
			Description: fmt.Sprintf("Scheduled via Leyendas Bot. Category: %s", category),
			StartTime:   startTime,
			EndTime:     endTime,
			TimeZone:    "UTC",
			Attendees:   emails,
		})
		if err != nil {
			return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to create Google Calendar event: %v", err)}, nil
		}
		meetLink = calRes.MeetLink
		htmlURL = calRes.HTMLURL
	}

	// Create row in Notion Meeting Notes
	notionMeeting, err := e.notionClient.CreateMeeting(ctx, e.meetingsDBID, notion.CreateMeetingParams{
		MeetingName:        meetingName,
		Date:               startTime.Format(time.RFC3339),
		Category:           category,
		DiscussionNotesURL: "",
		AttendeePageIDs:    attendeePageIDs,
	})
	if err != nil {
		// Log but don't fail entirely if calendar succeeded
		fmt.Printf("[WARN] Failed to create Notion meeting notes row: %v\n", err)
	}

	msg := fmt.Sprintf("🗓️ <b>Meeting Scheduled!</b>\n\n• <b>Title:</b> %s\n• <b>Time:</b> %s - %s\n• <b>Category:</b> %s",
		meetingName, startTime.Format("Mon Jan 2 15:04 MST"), endTime.Format("15:04 MST"), category)

	if meetLink != "" {
		msg += fmt.Sprintf("\n• 📹 <b>Google Meet:</b> <a href=\"%s\">%s</a>", meetLink, meetLink)
	}
	if htmlURL != "" {
		msg += fmt.Sprintf("\n• 📅 <b>Calendar Event:</b> <a href=\"%s\">Open in Google Calendar</a>", htmlURL)
	}
	if notionMeeting != nil {
		msg += "\n• 📝 Logged in Notion Meeting Notes"
	}

	return &ToolResult{
		Success: true,
		Message: msg,
		Data: map[string]any{
			"meet_link": meetLink,
			"start":     startTime.Format(time.RFC3339),
			"end":       endTime.Format(time.RFC3339),
		},
	}, nil
}

func (e *Executor) execGetUpcomingMeetings(ctx context.Context) (*ToolResult, error) {
	meetings, err := e.notionClient.QueryUpcomingMeetings(ctx, e.meetingsDBID)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to query meetings: %v", err)}, nil
	}

	if len(meetings) == 0 {
		return &ToolResult{Success: true, Message: "🗓️ No upcoming meetings found."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🗓️ <b>Upcoming Meetings (%d)</b>:\n\n", len(meetings)))

	for i, m := range meetings {
		dateStr := "TBD"
		if m.Date != nil {
			dateStr = m.Date.Format("Mon Jan 2 15:04")
		} else if m.DateStr != "" {
			dateStr = m.DateStr
		}

		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   📅 Date: %s | 🏷️ %s\n", i+1, m.MeetingName, dateStr, m.Category))
		if m.DiscussionNotesURL != "" {
			sb.WriteString(fmt.Sprintf("   📝 <a href=\"%s\">Discussion Notes Doc</a>\n", m.DiscussionNotesURL))
		}
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (e *Executor) execGetWorkspaceStatus(ctx context.Context) (*ToolResult, error) {
	tasks, err := e.notionClient.QueryOpenTasks(ctx, e.tasksDBID)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to query status: %v", err)}, nil
	}

	meetings, _ := e.notionClient.QueryUpcomingMeetings(ctx, e.meetingsDBID)

	var sb strings.Builder
	sb.WriteString("📊 <b>Leyendas Workspace Overview</b>\n\n")
	sb.WriteString(fmt.Sprintf("• 📋 <b>Open Tasks:</b> %d\n", len(tasks)))

	// Find next deadline
	var nextTask *notion.Task
	for _, t := range tasks {
		if t.DueDate != nil {
			if nextTask == nil || t.DueDate.Before(*nextTask.DueDate) {
				nextTask = t
			}
		}
	}
	if nextTask != nil {
		sb.WriteString(fmt.Sprintf("• ⏰ <b>Next Deadline:</b> %s (%s)\n", nextTask.TaskName, nextTask.DueDateStr))
	} else {
		sb.WriteString("• ⏰ <b>Next Deadline:</b> None set\n")
	}

	// Next meeting
	if len(meetings) > 0 {
		m := meetings[0]
		dateStr := m.DateStr
		if m.Date != nil {
			dateStr = m.Date.Format("Mon Jan 2 15:04")
		}
		sb.WriteString(fmt.Sprintf("• 🗓️ <b>Next Meeting:</b> %s (%s)\n", m.MeetingName, dateStr))
	} else {
		sb.WriteString("• 🗓️ <b>Next Meeting:</b> None scheduled\n")
	}

	return &ToolResult{Success: true, Message: sb.String()}, nil
}

func (e *Executor) execGetMyIdentity(sender *db.TeamMember) (*ToolResult, error) {
	if sender == nil {
		return &ToolResult{
			Success: false,
			Message: "❌ You are not registered. Please run <code>/register &lt;your-google-email&gt;</code> in a direct message.",
		}, nil
	}

	msg := fmt.Sprintf("👤 <b>Your Identity Record:</b>\n\n• <b>Full Name:</b> %s\n• <b>Telegram ID:</b> <code>%d</code>\n• <b>Telegram Handle:</b> @%s\n• <b>Google Email:</b> %s\n• <b>Role / Module:</b> %s\n• <b>Notion Page ID:</b> <code>%s</code>",
		sender.FullName, sender.TelegramUserID, sender.TelegramUsername, sender.GoogleEmail, sender.RoleModule, sender.NotionPageID)

	return &ToolResult{Success: true, Message: msg}, nil
}

func (e *Executor) execRegisterMember(ctx context.Context, sender *db.TeamMember, args map[string]any) (*ToolResult, error) {
	if sender == nil {
		return &ToolResult{Success: false, Message: "Cannot register without sender Telegram context."}, nil
	}

	email, _ := args["google_email"].(string)
	if !strings.Contains(email, "@") {
		return &ToolResult{Success: false, Message: "Please provide a valid Google email address."}, nil
	}

	role, _ := args["role_module"].(string)
	if role == "" {
		role = "Team Member"
	}

	// 1. Create page in Notion "The Team" DB
	pageID, err := e.notionClient.CreateTeamMemberPage(ctx, e.teamDBID, sender.FullName, sender.TelegramUsername, sender.TelegramUserID, email, role)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to create profile page in Notion: %v", err)}, nil
	}

	// 2. Save in Turso DB
	sender.NotionPageID = pageID
	sender.GoogleEmail = email
	sender.RoleModule = role

	if err := e.store.UpsertMember(ctx, sender); err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to save member in database: %v", err)}, nil
	}

	msg := fmt.Sprintf("🎉 <b>Registration Complete!</b>\n\nWelcome to Leyendas, <b>%s</b>!\n• Email: %s\n• Role: %s\n• Notion Page: <code>%s</code>\n\nYou can now be assigned tasks, schedule meetings, and interact with the AI assistant.",
		sender.FullName, email, role, pageID)

	return &ToolResult{Success: true, Message: msg}, nil
}

func (e *Executor) execTriggerStandupRound(ctx context.Context) (*ToolResult, error) {
	today := time.Now().Format("2006-01-02")
	round, err := e.store.GetActiveStandupRound(ctx, today)
	if err != nil {
		return &ToolResult{Success: false, Message: fmt.Sprintf("Failed to query standup: %v", err)}, nil
	}

	if round == nil {
		return &ToolResult{
			Success: true,
			Message: fmt.Sprintf("🌅 Standup round for %s is ready to be initiated with /standup in the group chat.", today),
		}, nil
	}

	replies, _ := e.store.GetStandupRepliesForRound(ctx, round.ID)
	msg := fmt.Sprintf("🌅 <b>Daily Standup — %s</b>\n• Status: %s\n• Replies received: %d",
		today, map[bool]string{true: "Closed", false: "Open"}[round.IsClosed], len(replies))

	return &ToolResult{Success: true, Message: msg}, nil
}

// UnmarshalArgsHelper turns map[string]any into strongly-typed structs
func UnmarshalArgsHelper(args map[string]any, target any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
