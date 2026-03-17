package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/schema"
	"github.com/tmc/langchaingo/textsplitter"
)

type InputDocument struct {
	SourceID string
	Title    string
	Content  string
	URL      string
	Tags     []string
}

type Service struct {
	db       *sql.DB
	embedder embeddings.Embedder
	splitter textsplitter.TextSplitter
}

func NewService(db *sql.DB, embedder embeddings.Embedder) (*Service, error) {
	s := &Service{
		db:       db,
		embedder: embedder,
		splitter: textsplitter.NewRecursiveCharacter(
			textsplitter.WithChunkSize(700),
			textsplitter.WithChunkOverlap(100),
		),
	}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) ensureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS rag_chunks (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    title TEXT,
    url TEXT,
    tags TEXT,
    content TEXT NOT NULL,
    embedding_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rag_chunks_source ON rag_chunks(source_id);
`
	_, err := s.db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("failed to ensure rag schema: %w", err)
	}
	return nil
}

func (s *Service) ReplaceIndex(ctx context.Context, docs []InputDocument) (int, error) {
	converted := make([]schema.Document, 0, len(docs))
	for _, d := range docs {
		converted = append(converted, schema.Document{
			PageContent: d.Content,
			Metadata: map[string]any{
				"source_id": d.SourceID,
				"title":     d.Title,
				"url":       d.URL,
				"tags":      strings.Join(d.Tags, ","),
			},
		})
	}

	chunked, err := textsplitter.SplitDocuments(s.splitter, converted)
	if err != nil {
		return 0, fmt.Errorf("failed to split documents: %w", err)
	}
	if len(chunked) == 0 {
		return 0, nil
	}

	textBatch := make([]string, 0, len(chunked))
	for _, doc := range chunked {
		textBatch = append(textBatch, doc.PageContent)
	}

	vectors, err := s.embedder.EmbedDocuments(ctx, textBatch)
	if err != nil {
		return 0, fmt.Errorf("failed to embed chunks: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM rag_chunks"); err != nil {
		return 0, fmt.Errorf("failed to clear rag_chunks: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO rag_chunks (id, source_id, title, url, tags, content, embedding_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for i, doc := range chunked {
		sourceID, _ := doc.Metadata["source_id"].(string)
		title, _ := doc.Metadata["title"].(string)
		url, _ := doc.Metadata["url"].(string)
		tags, _ := doc.Metadata["tags"].(string)

		embJSON, err := json.Marshal(vectors[i])
		if err != nil {
			return 0, fmt.Errorf("failed to encode embedding: %w", err)
		}

		chunkID := fmt.Sprintf("%s:%06d", sourceID, i)
		if _, err := stmt.ExecContext(ctx, chunkID, sourceID, title, url, tags, doc.PageContent, string(embJSON), now); err != nil {
			return 0, fmt.Errorf("failed to insert chunk: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit rag index: %w", err)
	}
	return len(chunked), nil
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]schema.Document, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []schema.Document{}, nil
	}
	if limit <= 0 {
		limit = 5
	}

	qVec, err := s.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT source_id, title, url, tags, content, embedding_json
FROM rag_chunks
`)
	if err != nil {
		return nil, fmt.Errorf("failed to query rag chunks: %w", err)
	}
	defer rows.Close()

	tokens := tokenize(query)

	type scored struct {
		doc      schema.Document
		score    float64
		vecScore float64
		lexScore float64
	}
	candidates := make([]scored, 0)

	for rows.Next() {
		var sourceID, title, url, tags, content, embeddingJSON string
		if err := rows.Scan(&sourceID, &title, &url, &tags, &content, &embeddingJSON); err != nil {
			return nil, fmt.Errorf("failed to scan rag chunk: %w", err)
		}

		var vec []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &vec); err != nil {
			continue
		}

		vecScore := cosine(qVec, vec)
		lexScore := lexicalScore(tokens, content)
		score := (0.75 * vecScore) + (0.25 * lexScore)
		if score <= 0 {
			continue
		}

		candidates = append(candidates, scored{
			score:    score,
			vecScore: vecScore,
			lexScore: lexScore,
			doc: schema.Document{
				PageContent: content,
				Score:       float32(score),
				Metadata: map[string]any{
					"source_id": sourceID,
					"title":     title,
					"url":       url,
					"tags":      tags,
					"vec_score": vecScore,
					"lex_score": lexScore,
				},
			},
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	results := make([]schema.Document, 0, limit)
	for _, cand := range candidates {
		results = append(results, cand.doc)
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func tokenize(input string) []string {
	parts := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) >= 2 {
			out = append(out, p)
		}
	}
	return out
}

func lexicalScore(queryTokens []string, content string) float64 {
	if len(queryTokens) == 0 || strings.TrimSpace(content) == "" {
		return 0
	}
	contentTokens := tokenize(content)
	if len(contentTokens) == 0 {
		return 0
	}

	contentSet := make(map[string]struct{}, len(contentTokens))
	for _, t := range contentTokens {
		contentSet[t] = struct{}{}
	}

	querySet := make(map[string]struct{}, len(queryTokens))
	for _, t := range queryTokens {
		querySet[t] = struct{}{}
	}

	intersection := 0
	unionSet := make(map[string]struct{}, len(contentSet)+len(querySet))
	for t := range contentSet {
		unionSet[t] = struct{}{}
	}
	for t := range querySet {
		if _, ok := contentSet[t]; ok {
			intersection++
		}
		unionSet[t] = struct{}{}
	}

	if len(unionSet) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(unionSet))
}
