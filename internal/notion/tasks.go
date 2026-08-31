package notion

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Task represents a strongly-typed Notion Task row.
type Task struct {
	ID             string     `json:"id"`
	URL            string     `json:"url"`
	TaskName       string     `json:"task_name"`
	AssigneePageID string     `json:"assignee_page_id,omitempty"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority,omitempty"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	DueDateStr     string     `json:"due_date_str,omitempty"`
	TaskType       string     `json:"task_type,omitempty"`
	Description    string     `json:"description,omitempty"`
	MeetingPageID  string     `json:"meeting_page_id,omitempty"`
	LastEditedTime time.Time  `json:"last_edited_time"`
}

// CreateTaskParams contains arguments to create a new task in Notion.
type CreateTaskParams struct {
	TaskName             string
	AssigneeNotionPageID string
	TaskType             string
	DueDate              string // YYYY-MM-DD or RFC3339
	Priority             string
	Description          string
	MeetingNotionPageID  string
}

// QueryOpenTasks returns all tasks where Status is NOT "Done".
func (c *Client) QueryOpenTasks(ctx context.Context, tasksDBID string) ([]*Task, error) {
	req := &QueryDatabaseRequest{
		Filter: map[string]any{
			"property": "Status",
			"select": map[string]any{
				"does_not_equal": "Done",
			},
		},
		Sorts: []map[string]any{
			{"property": "Due date", "direction": "ascending"},
		},
		PageSize: 100,
	}

	resp, err := c.QueryDatabase(ctx, tasksDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query open tasks: %w", err)
	}

	return parseTasks(resp.Results), nil
}

// QueryAllTasks returns all tasks in the database.
func (c *Client) QueryAllTasks(ctx context.Context, tasksDBID string) ([]*Task, error) {
	req := &QueryDatabaseRequest{
		Sorts: []map[string]any{
			{"property": "Due date", "direction": "ascending"},
		},
		PageSize: 100,
	}

	resp, err := c.QueryDatabase(ctx, tasksDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query all tasks: %w", err)
	}

	return parseTasks(resp.Results), nil
}

// QueryMyOpenTasks returns open tasks assigned to a specific The Team member page ID.
func (c *Client) QueryMyOpenTasks(ctx context.Context, tasksDBID, memberPageID string) ([]*Task, error) {
	req := &QueryDatabaseRequest{
		Filter: map[string]any{
			"and": []map[string]any{
				{
					"property": "Status",
					"select": map[string]any{
						"does_not_equal": "Done",
					},
				},
				{
					"property": "Assignee",
					"relation": map[string]any{
						"contains": memberPageID,
					},
				},
			},
		},
		Sorts: []map[string]any{
			{"property": "Due date", "direction": "ascending"},
		},
		PageSize: 100,
	}

	resp, err := c.QueryDatabase(ctx, tasksDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query personal tasks: %w", err)
	}

	return parseTasks(resp.Results), nil
}

// FindTaskByNameOrID searches for a task by its UUID or fuzzy name match.
func (c *Client) FindTaskByNameOrID(ctx context.Context, tasksDBID, query string) (*Task, error) {
	all, err := c.QueryAllTasks(ctx, tasksDBID)
	if err != nil {
		return nil, err
	}

	qLower := strings.ToLower(strings.TrimSpace(query))
	var exactMatch *Task
	var partialMatches []*Task

	for _, t := range all {
		if strings.EqualFold(t.ID, query) || strings.EqualFold(strings.ReplaceAll(t.ID, "-", ""), strings.ReplaceAll(query, "-", "")) {
			return t, nil
		}
		nameLower := strings.ToLower(t.TaskName)
		if nameLower == qLower {
			exactMatch = t
			break
		}
		if strings.Contains(nameLower, qLower) {
			partialMatches = append(partialMatches, t)
		}
	}

	if exactMatch != nil {
		return exactMatch, nil
	}
	if len(partialMatches) == 1 {
		return partialMatches[0], nil
	}
	if len(partialMatches) > 1 {
		var names []string
		for _, p := range partialMatches {
			names = append(names, fmt.Sprintf("• %s (ID: %s)", p.TaskName, p.ID))
		}
		return nil, fmt.Errorf("multiple tasks matched '%s':\n%s\nPlease be more specific.", query, strings.Join(names, "\n"))
	}

	return nil, fmt.Errorf("no task found matching '%s'", query)
}

// CreateTask creates a new row in the Tasks database.
func (c *Client) CreateTask(ctx context.Context, tasksDBID string, p CreateTaskParams) (*Task, error) {
	props := map[string]any{
		"Task name": map[string]any{
			"title": []RichTextObject{
				{Text: struct {
					Content string `json:"content"`
				}{Content: p.TaskName}},
			},
		},
		"Status": map[string]any{
			"select": map[string]string{"name": "Not started"},
		},
	}

	if p.AssigneeNotionPageID != "" {
		props["Assignee"] = map[string]any{
			"relation": []RelationItem{{ID: p.AssigneeNotionPageID}},
		}
	}

	if p.Priority != "" {
		props["Priority"] = map[string]any{
			"select": map[string]string{"name": p.Priority},
		}
	}

	if p.TaskType != "" {
		props["Task type"] = map[string]any{
			"select": map[string]string{"name": p.TaskType},
		}
	}

	if p.DueDate != "" {
		props["Due date"] = map[string]any{
			"date": DateValue{Start: p.DueDate},
		}
	}

	if p.Description != "" {
		props["Description"] = map[string]any{
			"rich_text": []RichTextObject{
				{Text: struct {
					Content string `json:"content"`
				}{Content: p.Description}},
			},
		}
	}

	if p.MeetingNotionPageID != "" {
		props["Meeting"] = map[string]any{
			"relation": []RelationItem{{ID: p.MeetingNotionPageID}},
		}
	}

	req := &CreatePageRequest{
		Properties: props,
	}
	req.Parent.DatabaseID = tasksDBID

	page, err := c.CreatePage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create task in Notion: %w", err)
	}

	return parseTask(*page), nil
}

// MarkTaskDone sets an existing task's Status to Done.
func (c *Client) MarkTaskDone(ctx context.Context, pageID string) (*Task, error) {
	req := &UpdatePageRequest{
		Properties: map[string]any{
			"Status": map[string]any{
				"select": map[string]string{"name": "Done"},
			},
		},
	}

	page, err := c.UpdatePageProperties(ctx, pageID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update task status to Done: %w", err)
	}

	return parseTask(*page), nil
}

func parseTasks(pages []Page) []*Task {
	var tasks []*Task
	for _, p := range pages {
		tasks = append(tasks, parseTask(p))
	}
	return tasks
}

func parseTask(p Page) *Task {
	t := &Task{
		ID:             p.ID,
		URL:            p.URL,
		LastEditedTime: p.LastEditedTime,
		Status:         "Not started",
	}

	if titleProp, ok := p.Properties["Task name"]; ok && len(titleProp.Title) > 0 {
		t.TaskName = titleProp.Title[0].PlainText
		if t.TaskName == "" {
			t.TaskName = titleProp.Title[0].Text.Content
		}
	}

	if assigneeProp, ok := p.Properties["Assignee"]; ok && len(assigneeProp.Relation) > 0 {
		t.AssigneePageID = assigneeProp.Relation[0].ID
	}

	if statusProp, ok := p.Properties["Status"]; ok {
		if statusProp.Select != nil {
			t.Status = statusProp.Select.Name
		} else if statusProp.Status != nil {
			t.Status = statusProp.Status.Name
		}
	}

	if priorityProp, ok := p.Properties["Priority"]; ok && priorityProp.Select != nil {
		t.Priority = priorityProp.Select.Name
	}

	if typeProp, ok := p.Properties["Task type"]; ok && typeProp.Select != nil {
		t.TaskType = typeProp.Select.Name
	}

	if dateProp, ok := p.Properties["Due date"]; ok && dateProp.Date != nil {
		t.DueDateStr = dateProp.Date.Start
		if parsed, err := time.Parse(time.RFC3339, dateProp.Date.Start); err == nil {
			t.DueDate = &parsed
		} else if parsed, err := time.Parse("2006-01-02", dateProp.Date.Start); err == nil {
			t.DueDate = &parsed
		}
	}

	if descProp, ok := p.Properties["Description"]; ok && len(descProp.RichText) > 0 {
		t.Description = descProp.RichText[0].PlainText
		if t.Description == "" {
			t.Description = descProp.RichText[0].Text.Content
		}
	}

	if meetingProp, ok := p.Properties["Meeting"]; ok && len(meetingProp.Relation) > 0 {
		t.MeetingPageID = meetingProp.Relation[0].ID
	}

	return t
}
