package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	tgmd "github.com/Mad-Pixels/goldmark-tgmd"
	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

// Your Google API key

type GeminiModels int

const (
	Gemini_2_5_flash GeminiModels = iota
	Gemini_2_5_flash_lite
	Gemini_3_flash
	Gemma_3_12b
	Gemma_3_27b
)

// A map to store the string representation of each state
var Models = map[GeminiModels]string{
	Gemini_2_5_flash:      "gemini-2.5-flash",
	Gemini_2_5_flash_lite: "gemini-2.5-flash-lite",
	Gemini_3_flash:        "gemini-3-flash-preview",
	Gemma_3_12b:           "gemma-3-12b-it",
	Gemma_3_27b:           "gemma-3-27b-it",
}

// String implements the fmt.Stringer interface
func (n GeminiModels) String() string {
	return Models[n]
}

type Client struct {
	client *genai.Client
	model  string
}

func NewSDKClientFromEnv() (*genai.Client, error) {
	_ = godotenv.Load()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("GEMINI_API_KEY environment variable is empty")
	}

	gClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize gemini sdk client: %w", err)
	}

	return gClient, nil
}

func NewFromEnv(model string) (*Client, error) {
	gClient, err := NewSDKClientFromEnv()
	if err != nil {
		return nil, err
	}

	return &Client{client: gClient, model: model}, nil
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("gemini client is not initialized")
	}

	chat, err := c.client.Chats.Create(ctx, c.model, nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create chat: %w", err)
	}

	result, err := chat.SendMessage(ctx, genai.Part{Text: prompt})
	if err != nil {
		return "", fmt.Errorf("failed to send message to gemini: %w", err)
	}

	resBytes := debugPrint(result)
	response, err := decodeGeminiResponse(resBytes)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	md := tgmd.TGMD()
	if err := md.Convert([]byte(response), &buf); err != nil {
		return "", fmt.Errorf("failed to convert markdown response: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

func GeminiResponse(userRequest string, model string, chatt *genai.Chat) (string, *genai.Chat) {

	type Part struct {
		Text string `json:"text"`
		Role string `json:"role"`
	}

	type Content struct {
		Parts []Part `json:"parts"`
		Role  string `json:"role"`
	}

	type Candidate struct {
		Content      Content `json:"content"`
		FinishReason string  `json:"finishReason"`
	}
	var GeminiRes struct {
		Url string `json:"url"`

		Candidates []Candidate `json:"candidates"`
	}

	err := godotenv.Load()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		panic("TOKEN environment variable is empty")
	}
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		panic(err)
	}

	chat, err := client.Chats.Create(ctx, model, nil, chatt.History(true))
	if err != nil {
		panic(err)
	}

	result, err := chat.SendMessage(ctx, genai.Part{Text: userRequest})
	if err != nil {
		panic(err)
	}

	geminiRes := debugPrint(result)

	if err := json.Unmarshal(geminiRes, &GeminiRes); err != nil {
		panic(err)
	}

	response := GeminiRes.Candidates[0].Content.Parts[0].Text

	var buf bytes.Buffer
	md := tgmd.TGMD()

	err = md.Convert([]byte(response), &buf)
	if err != nil {
		panic(err)
	}

	return buf.String(), chat

}

func decodeGeminiResponse(raw []byte) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type candidate struct {
		Content content `json:"content"`
	}
	var payload struct {
		Candidates []candidate `json:"candidates"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("failed to decode gemini payload: %w", err)
	}

	if len(payload.Candidates) == 0 || len(payload.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini response has no content")
	}

	return payload.Candidates[0].Content.Parts[0].Text, nil
}

func debugPrint[T any](r *T) []byte {

	response, err := json.Marshal(*r)
	if err != nil {
		panic(err)
	}

	return response
}
