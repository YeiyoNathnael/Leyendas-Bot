package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientRateLimitingAndRetry(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current == 1 {
			// First attempt returns 429
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"object":"error","status":429,"message":"rate limited"}`))
			return
		}

		// Second attempt succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","results":[],"has_more":false}`))
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.SetBaseURL(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.QueryDatabase(ctx, "test-db", &QueryDatabaseRequest{})
	if err != nil {
		t.Fatalf("expected successful query after retry, got error: %v", err)
	}

	if resp == nil || len(resp.Results) != 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestTasksCRUDPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pages":
			var body CreatePageRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode create page body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Validate database ID
			if body.Parent.DatabaseID != "tasks-db-123" {
				t.Errorf("unexpected database ID: %s", body.Parent.DatabaseID)
			}

			// Return mock page
			resp := Page{
				ID:             "task-created-uuid",
				CreatedTime:    time.Now(),
				LastEditedTime: time.Now(),
				Properties: map[string]PropertyRaw{
					"Task name": {
						Title: []RichTextObject{
							{PlainText: "Implement Login Redesign"},
						},
					},
					"Status": {
						Select: &SelectOption{Name: "Not started"},
					},
					"Priority": {
						Select: &SelectOption{Name: "High"},
					},
					"Due date": {
						Date: &DateValue{Start: "2026-09-04"},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPatch && r.URL.Path == "/pages/task-created-uuid":
			resp := Page{
				ID: "task-created-uuid",
				Properties: map[string]PropertyRaw{
					"Task name": {
						Title: []RichTextObject{
							{PlainText: "Implement Login Redesign"},
						},
					},
					"Status": {
						Select: &SelectOption{Name: "Done"},
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.SetBaseURL(server.URL)

	ctx := context.Background()

	// 1. Create Task
	created, err := client.CreateTask(ctx, "tasks-db-123", CreateTaskParams{
		TaskName:             "Implement Login Redesign",
		AssigneeNotionPageID: "member-page-abc",
		Priority:             "High",
		DueDate:              "2026-09-04",
	})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if created == nil || created.TaskName != "Implement Login Redesign" || created.Status != "Not started" {
		t.Fatalf("unexpected created task: %+v", created)
	}

	// 2. Mark Task Done
	done, err := client.MarkTaskDone(ctx, created.ID)
	if err != nil {
		t.Fatalf("MarkTaskDone failed: %v", err)
	}
	if done == nil || done.Status != "Done" {
		t.Fatalf("expected status Done, got %+v", done)
	}
}

func TestTeamAndMeetingsOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/pages" {
			resp := Page{
				ID: "created-entity-uuid",
				Properties: map[string]PropertyRaw{
					"Meeting name": {
						Title: []RichTextObject{{PlainText: "Capstone Sync"}},
					},
					"Category": {
						Select: &SelectOption{Name: "Planning"},
					},
					"Discussion Notes": {
						URL: ptr("https://docs.google.com/document/d/123"),
					},
				},
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.SetBaseURL(server.URL)

	ctx := context.Background()

	// 1. Create Team Member
	pageID, err := client.CreateTeamMemberPage(ctx, "team-db", "Nathnael", "yeiyo", 12345, "yeiyo@test.com", "Dev")
	if err != nil || pageID != "created-entity-uuid" {
		t.Fatalf("CreateTeamMemberPage failed: %v, pageID: %s", err, pageID)
	}

	// 2. Create Meeting
	meeting, err := client.CreateMeeting(ctx, "meetings-db", CreateMeetingParams{
		MeetingName:        "Capstone Sync",
		Category:           "Planning",
		Date:               "2026-09-01T10:00:00Z",
		DiscussionNotesURL: "https://docs.google.com/document/d/123",
		AttendeePageIDs:    []string{pageID},
	})
	if err != nil {
		t.Fatalf("CreateMeeting failed: %v", err)
	}
	if meeting == nil || meeting.MeetingName != "Capstone Sync" || meeting.Category != "Planning" {
		t.Fatalf("unexpected meeting data: %+v", meeting)
	}
}

func ptr[T any](v T) *T {
	return &v
}
