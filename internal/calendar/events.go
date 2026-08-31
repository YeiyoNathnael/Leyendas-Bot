package calendar

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/api/calendar/v3"
)

// CreateMeetingParams contains parameters for scheduling a meeting.
type CreateMeetingParams struct {
	Summary     string
	Description string
	StartTime   time.Time
	EndTime     time.Time
	TimeZone    string
	Attendees   []string // List of attendee Google email addresses
}

// CreatedMeetingResult contains the created Google Calendar event details and Meet link.
type CreatedMeetingResult struct {
	EventID   string    `json:"event_id"`
	Summary   string    `json:"summary"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	MeetLink  string    `json:"meet_link"`
	HTMLURL   string    `json:"html_url"`
}

// CreateEventWithMeet creates a Google Calendar event with an auto-generated Google Meet video conference link.
func (s *Service) CreateEventWithMeet(ctx context.Context, calendarID string, params CreateMeetingParams) (*CreatedMeetingResult, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	if params.TimeZone == "" {
		params.TimeZone = "UTC"
	}

	var attendees []*calendar.EventAttendee
	for _, email := range params.Attendees {
		if email != "" {
			attendees = append(attendees, &calendar.EventAttendee{Email: email})
		}
	}

	requestID := uuid.New().String()

	event := &calendar.Event{
		Summary:     params.Summary,
		Description: params.Description,
		Start: &calendar.EventDateTime{
			DateTime: params.StartTime.Format(time.RFC3339),
			TimeZone: params.TimeZone,
		},
		End: &calendar.EventDateTime{
			DateTime: params.EndTime.Format(time.RFC3339),
			TimeZone: params.TimeZone,
		},
		Attendees: attendees,
		ConferenceData: &calendar.ConferenceData{
			CreateRequest: &calendar.CreateConferenceRequest{
				RequestId: requestID,
				ConferenceSolutionKey: &calendar.ConferenceSolutionKey{
					Type: "hangoutsMeet",
				},
			},
		},
	}

	// ConferenceDataVersion(1) is MANDATORY for Google to generate a Meet link
	call := s.srv.Events.Insert(calendarID, event).
		ConferenceDataVersion(1).
		SendUpdates("all").
		Context(ctx)

	created, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("failed to insert calendar event: %w", err)
	}

	// Extract Meet link
	meetLink := created.HangoutLink
	if meetLink == "" && created.ConferenceData != nil {
		for _, ep := range created.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				meetLink = ep.Uri
				break
			}
		}
	}

	return &CreatedMeetingResult{
		EventID:   created.Id,
		Summary:   created.Summary,
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		MeetLink:  meetLink,
		HTMLURL:   created.HtmlLink,
	}, nil
}
