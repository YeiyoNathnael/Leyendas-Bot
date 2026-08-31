package ai

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
	"leyendas-bot/internal/db"
)

// Agent manages interactions with the Gemma 4 31B AI model via the Google GenAI SDK.
type Agent struct {
	client    *genai.Client
	modelName string
	executor  *Executor
}

// NewAgent initializes a new AI Agent instance.
func NewAgent(ctx context.Context, apiKey, modelName string, executor *Executor) (*Agent, error) {
	if modelName == "" {
		modelName = "gemma-4-31b"
	}

	var client *genai.Client
	var err error

	if apiKey != "" {
		client, err = genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		})
	} else {
		// Reads GEMINI_API_KEY from environment automatically
		client, err = genai.NewClient(ctx, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GenAI client: %w", err)
	}

	return &Agent{
		client:    client,
		modelName: modelName,
		executor:  executor,
	}, nil
}

// ProcessNaturalLanguageMessage handles a user message, resolves tools, and returns the response string.
func (a *Agent) ProcessNaturalLanguageMessage(
	ctx context.Context,
	sender *db.TeamMember,
	userMessage string,
) (string, error) {
	if a.client == nil {
		return "❌ AI integration is not configured (missing API key).", nil
	}

	systemInstruction := `You are the Leyendas Bot AI assistant for the Leyendas Capstone workspace.
Your job is to assist team members with task management, meeting scheduling, and status queries.
Guidelines:
1. When a user asks to create, update, list tasks, schedule meetings, or query status, call the appropriate tool.
2. The sender's identity is provided in the prompt context. Always default the assignee to the sender unless another member is explicitly named.
3. If no tool applies or the user is asking a general question, reply with a helpful, friendly message.
4. Never call more than one tool per message.
5. If required parameters are ambiguous or missing, politely ask the user for clarification instead of guessing.`

	senderContext := "Sender: Unregistered User"
	if sender != nil {
		senderContext = fmt.Sprintf("Sender: %s (Telegram ID: %d, Notion Page ID: '%s', Google Email: '%s', Role: '%s')",
			sender.FullName, sender.TelegramUserID, sender.NotionPageID, sender.GoogleEmail, sender.RoleModule)
	}

	fullPrompt := fmt.Sprintf("%s\n\nUser Message: %s", senderContext, userMessage)

	toolDecls := GetToolDeclarations()
	config := &genai.GenerateContentConfig{
		Tools: []*genai.Tool{
			{
				FunctionDeclarations: toolDecls,
			},
		},
		ToolConfig: &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		},
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: systemInstruction},
			},
		},
	}

	contents := []*genai.Content{
		{
			Role: "user",
			Parts: []*genai.Part{
				{Text: fullPrompt},
			},
		},
	}

	resp, err := a.client.Models.GenerateContent(ctx, a.modelName, contents, config)
	if err != nil {
		return "", fmt.Errorf("GenAI generate error: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "⚠️ Received an empty response from the AI model.", nil
	}

	candidate := resp.Candidates[0]
	contents = append(contents, candidate.Content)

	var functionCalls []*genai.FunctionCall
	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}

	if len(functionCalls) > 0 {
		var toolResponses []*genai.Part
		var executionMessages []string

		for _, call := range functionCalls {
			result, err := a.executor.ExecuteTool(ctx, sender, call.Name, call.Args)
			if err != nil {
				result = &ToolResult{Success: false, Message: err.Error()}
			}

			if result.Message != "" {
				executionMessages = append(executionMessages, result.Message)
			}

			execMap := map[string]any{
				"success": result.Success,
				"message": result.Message,
			}
			if result.Data != nil {
				execMap["data"] = result.Data
			}

			toolResponses = append(toolResponses, genai.NewPartFromFunctionResponse(call.Name, execMap))
		}

		// Tool action results are already nicely formatted for Telegram
		if len(executionMessages) > 0 {
			return strings.Join(executionMessages, "\n\n"), nil
		}

		// Optional follow-up turn if no direct message was provided
		contents = append(contents, &genai.Content{
			Role:  "user",
			Parts: toolResponses,
		})

		finalResp, err := a.client.Models.GenerateContent(ctx, a.modelName, contents, config)
		if err == nil && len(finalResp.Candidates) > 0 && finalResp.Candidates[0].Content != nil {
			for _, p := range finalResp.Candidates[0].Content.Parts {
				if p.Text != "" {
					return p.Text, nil
				}
			}
		}

		return "✅ Action executed successfully.", nil
	}

	// Plain text reply
	var replyTexts []string
	for _, p := range candidate.Content.Parts {
		if p.Text != "" {
			replyTexts = append(replyTexts, p.Text)
		}
	}

	if len(replyTexts) > 0 {
		return strings.Join(replyTexts, "\n"), nil
	}

	return "👍 Got it.", nil
}
