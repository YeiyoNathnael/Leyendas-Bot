package notion

import "time"

// Notion API constants
const (
	APIBaseURL = "https://api.notion.com/v1"
	APIVersion = "2022-06-28"
)

// RichTextObject represents Notion rich text elements.
type RichTextObject struct {
	Type string `json:"type,omitempty"`
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
	PlainText string `json:"plain_text,omitempty"`
}

// RelationItem represents a reference to another Notion page ID.
type RelationItem struct {
	ID string `json:"id"`
}

// DateValue represents a Notion date property value.
type DateValue struct {
	Start string  `json:"start"`
	End   *string `json:"end,omitempty"`
}

// SelectOption represents a select or status option.
type SelectOption struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Page represents a Notion page object.
type Page struct {
	ID             string                 `json:"id"`
	CreatedTime    time.Time              `json:"created_time"`
	LastEditedTime time.Time              `json:"last_edited_time"`
	Archived       bool                   `json:"archived"`
	URL            string                 `json:"url"`
	Properties     map[string]PropertyRaw `json:"properties"`
}

// PropertyRaw represents raw property data in a Notion page.
type PropertyRaw struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type"`
	Title    []RichTextObject `json:"title,omitempty"`
	RichText []RichTextObject `json:"rich_text,omitempty"`
	Select   *SelectOption    `json:"select,omitempty"`
	Status   *SelectOption    `json:"status,omitempty"`
	Date     *DateValue       `json:"date,omitempty"`
	Relation []RelationItem   `json:"relation,omitempty"`
	Number   *float64         `json:"number,omitempty"`
	Email    *string          `json:"email,omitempty"`
	URL      *string          `json:"url,omitempty"`
}

// QueryDatabaseRequest represents a query to /v1/databases/{id}/query.
type QueryDatabaseRequest struct {
	Filter      any    `json:"filter,omitempty"`
	Sorts       any    `json:"sorts,omitempty"`
	StartCursor string `json:"start_cursor,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
}

// QueryDatabaseResponse represents response from /v1/databases/{id}/query.
type QueryDatabaseResponse struct {
	Object     string `json:"object"`
	Results    []Page `json:"results"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// CreatePageRequest represents payload to POST /v1/pages.
type CreatePageRequest struct {
	Parent struct {
		DatabaseID string `json:"database_id"`
	} `json:"parent"`
	Properties map[string]any `json:"properties"`
}

// UpdatePageRequest represents payload to PATCH /v1/pages/{id}.
type UpdatePageRequest struct {
	Properties map[string]any `json:"properties,omitempty"`
	Archived   *bool          `json:"archived,omitempty"`
}
