package app

import (
	"strings"
	"testing"

	"github.com/tmc/langchaingo/schema"
)

func TestFormatSourceCitations(t *testing.T) {
	docs := []schema.Document{
		{Metadata: map[string]any{"title": "Sprint Plan", "url": "https://example.com/plan"}},
		{Metadata: map[string]any{"title": "Sprint Plan", "url": "https://example.com/plan"}},
		{Metadata: map[string]any{"title": "Calendar", "url": "https://example.com/cal"}},
	}
	out := formatSourceCitations(docs)
	if !strings.Contains(out, "Sources:") {
		t.Fatalf("expected Sources section, got %q", out)
	}
	if strings.Count(out, "https://example\\.com/plan") != 1 {
		t.Fatalf("expected deduplicated source entries, got %q", out)
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	in := "a_b-c"
	got := escapeMarkdownV2(in)
	want := "a\\_b\\-c"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
