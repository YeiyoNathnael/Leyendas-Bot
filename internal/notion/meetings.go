package notion

import (
	"context"
	"fmt"
	"time"
)

// Meeting represents a row in the Meeting Notes database.
type Meeting struct {
	ID                 string     `json:"id"`
	MeetingName        string     `json:"meeting_name"`
	Date               *time.Time `json:"date,omitempty"`
	DateStr            string     `json:"date_str,omitempty"`
	Category           string     `json:"category,omitempty"`
	DiscussionNotesURL string     `json:"discussion_notes_url,omitempty"`
	AttendeePageIDs    []string   `json:"attendee_page_ids,omitempty"`
	ActionItemTaskIDs  []string   `json:"action_item_task_ids,omitempty"`
}

// CreateMeetingParams contains parameters to create a new Meeting Notes row.
type CreateMeetingParams struct {
	MeetingName        string
	Date               string // ISO-8601 or RFC3339
	Category           string
	DiscussionNotesURL string
	AttendeePageIDs    []string
}

// QueryUpcomingMeetings returns meetings with Date on or after today.
func (c *Client) QueryUpcomingMeetings(ctx context.Context, meetingsDBID string) ([]*Meeting, error) {
	todayStr := time.Now().UTC().Format("2006-01-02")
	req := &QueryDatabaseRequest{
		Filter: map[string]any{
			"property": "Date",
			"date": map[string]any{
				"on_or_after": todayStr,
			},
		},
		Sorts: []map[string]any{
			{"property": "Date", "direction": "ascending"},
		},
		PageSize: 10,
	}

	resp, err := c.QueryDatabase(ctx, meetingsDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query upcoming meetings: %w", err)
	}

	return parseMeetings(resp.Results), nil
}

// QueryLatestMeeting returns the next upcoming or most recent meeting.
func (c *Client) QueryLatestMeeting(ctx context.Context, meetingsDBID string) (*Meeting, error) {
	req := &QueryDatabaseRequest{
		Sorts: []map[string]any{
			{"property": "Date", "direction": "descending"},
		},
		PageSize: 1,
	}

	resp, err := c.QueryDatabase(ctx, meetingsDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest meeting: %w", err)
	}

	if len(resp.Results) == 0 {
		return nil, nil
	}

	return parseMeeting(resp.Results[0]), nil
}

// CreateMeeting creates a new row in the Meeting Notes database.
func (c *Client) CreateMeeting(ctx context.Context, meetingsDBID string, p CreateMeetingParams) (*Meeting, error) {
	props := map[string]any{
		"Meeting name": map[string]any{
			"title": []RichTextObject{
				{Text: struct {
					Content string `json:"content"`
				}{Content: p.MeetingName}},
			},
		},
	}

	if p.Date != "" {
		props["Date"] = map[string]any{
			"date": DateValue{Start: p.Date},
		}
	}

	if p.Category != "" {
		props["Category"] = map[string]any{
			"select": map[string]string{"name": p.Category},
		}
	}

	if p.DiscussionNotesURL != "" {
		props["Discussion Notes"] = map[string]any{
			"url": p.DiscussionNotesURL,
		}
	}

	if len(p.AttendeePageIDs) > 0 {
		var rels []RelationItem
		for _, id := range p.AttendeePageIDs {
			rels = append(rels, RelationItem{ID: id})
		}
		props["Attendees"] = map[string]any{
			"relation": rels,
		}
	}

	req := &CreatePageRequest{
		Properties: props,
	}
	req.Parent.DatabaseID = meetingsDBID

	page, err := c.CreatePage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create meeting row in Notion: %w", err)
	}

	return parseMeeting(*page), nil
}

func parseMeetings(pages []Page) []*Meeting {
	var meetings []*Meeting
	for _, p := range pages {
		meetings = append(meetings, parseMeeting(p))
	}
	return meetings
}

func parseMeeting(p Page) *Meeting {
	m := &Meeting{
		ID: p.ID,
	}

	if titleProp, ok := p.Properties["Meeting name"]; ok && len(titleProp.Title) > 0 {
		m.MeetingName = titleProp.Title[0].PlainText
		if m.MeetingName == "" {
			m.MeetingName = titleProp.Title[0].Text.Content
		}
	}

	if dateProp, ok := p.Properties["Date"]; ok && dateProp.Date != nil {
		m.DateStr = dateProp.Date.Start
		if parsed, err := time.Parse(time.RFC3339, dateProp.Date.Start); err == nil {
			m.Date = &parsed
		} else if parsed, err := time.Parse("2006-01-02", dateProp.Date.Start); err == nil {
			m.Date = &parsed
		}
	}

	if catProp, ok := p.Properties["Category"]; ok && catProp.Select != nil {
		m.Category = catProp.Select.Name
	}

	if urlProp, ok := p.Properties["Discussion Notes"]; ok && urlProp.URL != nil {
		m.DiscussionNotesURL = *urlProp.URL
	}

	if attProp, ok := p.Properties["Attendees"]; ok {
		for _, r := range attProp.Relation {
			m.AttendeePageIDs = append(m.AttendeePageIDs, r.ID)
		}
	}

	if actProp, ok := p.Properties["Action Items"]; ok {
		for _, r := range actProp.Relation {
			m.ActionItemTaskIDs = append(m.ActionItemTaskIDs, r.ID)
		}
	}

	return m
}
