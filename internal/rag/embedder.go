package rag

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/embeddings"
	"google.golang.org/genai"
)

type GeminiEmbedder struct {
	client *genai.Client
	model  string
}

func NewGeminiEmbedder(client *genai.Client, model string) embeddings.Embedder {
	if model == "" {
		model = "text-embedding-004"
	}
	return &GeminiEmbedder{client: client, model: model}
}

func (g *GeminiEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	contents := make([]*genai.Content, 0, len(texts))
	for _, t := range texts {
		contents = append(contents, genai.NewContentFromText(t, ""))
	}

	res, err := g.client.Models.EmbedContent(ctx, g.model, contents, nil)
	if err != nil {
		return nil, fmt.Errorf("embed documents failed: %w", err)
	}
	if len(res.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed response size mismatch: got %d want %d", len(res.Embeddings), len(texts))
	}

	vectors := make([][]float32, 0, len(res.Embeddings))
	for _, emb := range res.Embeddings {
		vectors = append(vectors, emb.Values)
	}
	return vectors, nil
}

func (g *GeminiEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := g.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	return vectors[0], nil
}
