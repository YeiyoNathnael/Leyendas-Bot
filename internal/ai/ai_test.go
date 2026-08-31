package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"leyendas-bot/internal/db"
	"leyendas-bot/internal/notion"
)

func TestToolDeclarations(t *testing.T) {
	tools := GetToolDeclarations()
	if len(tools) != 11 {
		t.Fatalf("expected 11 tool declarations, got %d", len(tools))
	}

	expectedTools := map[string]bool{
		"create_task":            false,
		"mark_task_done":         false,
		"list_open_tasks":        false,
		"list_my_tasks":          false,
		"get_task_details":       false,
		"schedule_meeting":       false,
		"get_upcoming_meetings":  false,
		"get_workspace_status":   false,
		"get_my_identity":        false,
		"register_member":        false,
		"trigger_standup_round":  false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		} else {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestExecutorOperations(t *testing.T) {
	// Setup test store
	tempDir := t.TempDir()
	store, err := db.NewStore("file:"+filepath.Join(tempDir, "test.db"), "")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	// Setup mock Notion server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","results":[],"has_more":false}`))
	}))
	defer mockServer.Close()

	notionClient := notion.NewClient("token")
	notionClient.SetBaseURL(mockServer.URL)

	executor := NewExecutor(store, notionClient, nil, "tasks-db", "team-db", "meetings-db")

	sender := &db.TeamMember{
		FullName:         "Test User",
		TelegramUserID:   12345,
		TelegramUsername: "testuser",
		GoogleEmail:      "test@example.com",
		NotionPageID:     "notion-page-123",
		RoleModule:       "Engineer",
	}

	ctx := context.Background()

	// 1. Test get_my_identity
	res, err := executor.ExecuteTool(ctx, sender, "get_my_identity", nil)
	if err != nil || !res.Success {
		t.Fatalf("get_my_identity failed: %v, res: %+v", err, res)
	}

	// 2. Test list_open_tasks (empty list)
	res, err = executor.ExecuteTool(ctx, sender, "list_open_tasks", nil)
	if err != nil || !res.Success {
		t.Fatalf("list_open_tasks failed: %v, res: %+v", err, res)
	}

	// 3. Test trigger_standup_round
	res, err = executor.ExecuteTool(ctx, sender, "trigger_standup_round", nil)
	if err != nil || !res.Success {
		t.Fatalf("trigger_standup_round failed: %v, res: %+v", err, res)
	}
}
