package notion

import (
	"context"
	"fmt"
)

// TeamPageMember represents a member profile page read from The Team database.
type TeamPageMember struct {
	PageID           string  `json:"page_id"`
	FullName         string  `json:"full_name"`
	TelegramUsername string  `json:"telegram_username"`
	TelegramUserID   int64   `json:"telegram_user_id"`
	GoogleEmail      string  `json:"google_email"`
	RoleModule       string  `json:"role_module"`
}

// CreateTeamMemberPage creates a new profile page inside "The Team" database.
func (c *Client) CreateTeamMemberPage(
	ctx context.Context,
	teamDBID string,
	fullName string,
	telegramUsername string,
	telegramUserID int64,
	googleEmail string,
	roleModule string,
) (string, error) {
	tgIDFloat := float64(telegramUserID)

	props := map[string]any{
		"Name": map[string]any{
			"title": []RichTextObject{
				{Text: struct {
					Content string `json:"content"`
				}{Content: fullName}},
			},
		},
		"Telegram User ID": map[string]any{
			"number": tgIDFloat,
		},
		"Google Email": map[string]any{
			"email": googleEmail,
		},
	}

	if telegramUsername != "" {
		props["Telegram Username"] = map[string]any{
			"rich_text": []RichTextObject{
				{Text: struct {
					Content string `json:"content"`
				}{Content: telegramUsername}},
			},
		}
	}

	if roleModule != "" {
		props["Role / Module"] = map[string]any{
			"select": map[string]string{"name": roleModule},
		}
	}

	req := &CreatePageRequest{
		Properties: props,
	}
	req.Parent.DatabaseID = teamDBID

	page, err := c.CreatePage(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create team member page in Notion: %w", err)
	}

	return page.ID, nil
}

// QueryAllTeamMembers reads all profile pages from The Team database.
func (c *Client) QueryAllTeamMembers(ctx context.Context, teamDBID string) ([]*TeamPageMember, error) {
	req := &QueryDatabaseRequest{
		PageSize: 100,
	}

	resp, err := c.QueryDatabase(ctx, teamDBID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query team members database: %w", err)
	}

	var members []*TeamPageMember
	for _, p := range resp.Results {
		m := &TeamPageMember{
			PageID: p.ID,
		}

		if nameProp, ok := p.Properties["Name"]; ok && len(nameProp.Title) > 0 {
			m.FullName = nameProp.Title[0].PlainText
			if m.FullName == "" {
				m.FullName = nameProp.Title[0].Text.Content
			}
		}

		if tgUserProp, ok := p.Properties["Telegram Username"]; ok && len(tgUserProp.RichText) > 0 {
			m.TelegramUsername = tgUserProp.RichText[0].PlainText
		}

		if tgIDProp, ok := p.Properties["Telegram User ID"]; ok && tgIDProp.Number != nil {
			m.TelegramUserID = int64(*tgIDProp.Number)
		}

		if emailProp, ok := p.Properties["Google Email"]; ok && emailProp.Email != nil {
			m.GoogleEmail = *emailProp.Email
		}

		if roleProp, ok := p.Properties["Role / Module"]; ok && roleProp.Select != nil {
			m.RoleModule = roleProp.Select.Name
		}

		members = append(members, m)
	}

	return members, nil
}
