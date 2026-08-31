package ai

import "google.golang.org/genai"

// GetToolDeclarations returns all OpenAPI function declarations supported by Gemma 4 31B.
func GetToolDeclarations() []*genai.FunctionDeclaration {
	return []*genai.FunctionDeclaration{
		{
			Name:        "create_task",
			Description: "Create a new task row in the Notion Tasks database.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"task_name": {
						Type:        genai.TypeString,
						Description: "Name or title of the task.",
					},
					"assignee_notion_page_id": {
						Type:        genai.TypeString,
						Description: "Notion page ID of the registered team member assigned to this task (defaults to sender if not specified).",
					},
					"task_type": {
						Type:        genai.TypeString,
						Description: "Task classification.",
						Enum:        []string{"Feature", "Bug", "Research", "Documentation", "Other"},
					},
					"due_date": {
						Type:        genai.TypeString,
						Description: "Due date in YYYY-MM-DD or RFC3339 format.",
					},
					"priority": {
						Type:        genai.TypeString,
						Description: "Task priority urgency.",
						Enum:        []string{"Low", "Medium", "High"},
					},
					"description": {
						Type:        genai.TypeString,
						Description: "Detailed task description.",
					},
				},
				Required: []string{"task_name"},
			},
		},
		{
			Name:        "mark_task_done",
			Description: "Set an existing task's Status to Done in Notion (counterpart of /done).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"task_name_or_id": {
						Type:        genai.TypeString,
						Description: "The exact or partial name, or UUID of the task to mark as Done.",
					},
				},
				Required: []string{"task_name_or_id"},
			},
		},
		{
			Name:        "list_open_tasks",
			Description: "List all open tasks (Status != Done) in the Notion workspace, optionally filtered by assignee (counterpart of /tasks).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"assignee_name": {
						Type:        genai.TypeString,
						Description: "Optional member name filter.",
					},
				},
			},
		},
		{
			Name:        "list_my_tasks",
			Description: "List open tasks assigned to the current sender (counterpart of /mytasks).",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{},
			},
		},
		{
			Name:        "get_task_details",
			Description: "Get complete details and status of a specific task by name or ID (counterpart of /task).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"task_name_or_id": {
						Type:        genai.TypeString,
						Description: "The name or ID of the task to look up.",
					},
				},
				Required: []string{"task_name_or_id"},
			},
		},
		{
			Name:        "schedule_meeting",
			Description: "Create a Google Calendar event with auto-generated Google Meet link and log a Meeting Notes row in Notion (counterpart of /schedule).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"start_datetime": {
						Type:        genai.TypeString,
						Description: "Start date and time in ISO-8601 format (e.g. 2026-09-02T15:00:00Z).",
					},
					"duration_minutes": {
						Type:        genai.TypeInteger,
						Description: "Duration of the meeting in minutes (default: 30).",
					},
					"meeting_name": {
						Type:        genai.TypeString,
						Description: "Title or topic of the meeting.",
					},
					"category": {
						Type:        genai.TypeString,
						Description: "Meeting category.",
						Enum:        []string{"Standup", "Planning", "Review", "Ad-hoc"},
					},
				},
				Required: []string{"start_datetime"},
			},
		},
		{
			Name:        "trigger_standup_round",
			Description: "Manually initiate or check today's standup round in the group chat (counterpart of /standup).",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{},
			},
		},
		{
			Name:        "get_upcoming_meetings",
			Description: "Get upcoming meetings and their discussion notes doc links (counterpart of /agenda and /meetings).",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{},
			},
		},
		{
			Name:        "get_workspace_status",
			Description: "Get a quick overview of open tasks count, next deadline, and next scheduled meeting (counterpart of /status).",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{},
			},
		},
		{
			Name:        "get_my_identity",
			Description: "Get the current caller's resolved identity record (counterpart of /whoami).",
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: map[string]*genai.Schema{},
			},
		},
		{
			Name:        "register_member",
			Description: "Register the sender's identity in the team database (counterpart of /register).",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"google_email": {
						Type:        genai.TypeString,
						Description: "The member's Google email address.",
					},
					"role_module": {
						Type:        genai.TypeString,
						Description: "The member's role or project module.",
					},
				},
				Required: []string{"google_email"},
			},
		},
	}
}
