package embedding

import (
	"strings"
	"testing"
)

func TestChunk_SmallContentSingleChunk(t *testing.T) {
	cfg := ChunkConfig{MaxTokens: 1000, OverlapTokens: 50, MaxChunks: 10}
	got := Chunk("hello world", cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(got))
	}
	if got[0].Content != "hello world" {
		t.Fatalf("content altered: %q", got[0].Content)
	}
	if got[0].Index != 0 {
		t.Fatalf("want index 0, got %d", got[0].Index)
	}
}

func TestChunk_LargeContentSplits(t *testing.T) {
	body := strings.Repeat("paragraph one.\n\nparagraph two.\n\n", 500)
	cfg := ChunkConfig{MaxTokens: 64, OverlapTokens: 8, MaxChunks: 1000}
	got := Chunk(body, cfg)
	if len(got) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(got))
	}
	for i, c := range got {
		if c.Index != i {
			t.Fatalf("chunk %d has index %d", i, c.Index)
		}
		if c.Content == "" {
			t.Fatalf("chunk %d empty", i)
		}
	}
}

func TestChunk_NoContentLoss(t *testing.T) {
	body := strings.Repeat("The quick brown fox. ", 2000)
	cfg := ChunkConfig{MaxTokens: 32, OverlapTokens: 0, MaxChunks: 100000}
	got := Chunk(body, cfg)
	var sb strings.Builder
	for _, c := range got {
		sb.WriteString(c.Content)
	}
	if normalizeWS(sb.String()) != normalizeWS(body) {
		t.Fatalf("content lost: joined len=%d orig len=%d", sb.Len(), len(body))
	}
}

func TestChunk_MaxChunksAbsorbsRemainder(t *testing.T) {
	body := strings.Repeat("word ", 10000)
	cfg := ChunkConfig{MaxTokens: 16, OverlapTokens: 0, MaxChunks: 3}
	got := Chunk(body, cfg)
	if len(got) != 3 {
		t.Fatalf("want exactly 3 chunks (cap), got %d", len(got))
	}
	var sb strings.Builder
	for _, c := range got {
		sb.WriteString(c.Content)
	}
	if normalizeWS(sb.String()) != normalizeWS(body) {
		t.Fatalf("content dropped under MaxChunks cap")
	}
}

func TestChunk_Unicode(t *testing.T) {
	body := strings.Repeat("日本語のテキストです。", 1000)
	cfg := ChunkConfig{MaxTokens: 16, OverlapTokens: 0, MaxChunks: 100000}
	got := Chunk(body, cfg)
	for _, c := range got {
		if !validUTF8(c.Content) {
			t.Fatalf("chunk split a multibyte rune")
		}
	}
}

func normalizeWS(s string) string { return strings.Join(strings.Fields(s), " ") }
