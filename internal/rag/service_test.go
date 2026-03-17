package rag

import "testing"

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, Takhtit! sprint_42 planning")
	if len(tokens) == 0 {
		t.Fatal("expected non-empty tokens")
	}
	if tokens[0] != "hello" {
		t.Fatalf("expected first token hello, got %q", tokens[0])
	}
}

func TestLexicalScoreHigherForRelevantContent(t *testing.T) {
	queryTokens := tokenize("calendar sprint planning")
	relevant := lexicalScore(queryTokens, "Sprint planning meeting from calendar")
	irrelevant := lexicalScore(queryTokens, "binary tree graph shortest path")
	if relevant <= irrelevant {
		t.Fatalf("expected relevant score > irrelevant score, got relevant=%f irrelevant=%f", relevant, irrelevant)
	}
}

func TestCosineIdentity(t *testing.T) {
	v := []float32{0.1, 0.2, 0.3}
	s := cosine(v, v)
	if s < 0.9999 {
		t.Fatalf("expected cosine ~1, got %f", s)
	}
}
