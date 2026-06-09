package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestSummarizeCluster(t *testing.T) {
	mock := &mockLLM{response: `{"title":"Finance Cluster","summary":"Related finance patterns."}`}
	entries := []storage.KnowledgeEntry{
		{Title: "Entry 1", Content: "Finance content 1"},
		{Title: "Entry 2", Content: "Finance content 2"},
	}
	result, err := SummarizeCluster(context.Background(), mock, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "Finance Cluster" {
		t.Errorf("title = %q, want %q", result.Title, "Finance Cluster")
	}
	if result.Summary == "" {
		t.Error("summary is empty")
	}
}

func TestSummarizeCluster_MarkdownFence(t *testing.T) {
	mock := &mockLLM{response: "```json\n{\"title\":\"T\",\"summary\":\"S\"}\n```"}
	entries := []storage.KnowledgeEntry{{Title: "E", Content: "C"}}
	result, err := SummarizeCluster(context.Background(), mock, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Title != "T" {
		t.Errorf("title = %q, want T", result.Title)
	}
}

func TestSummarizeCluster_LLMError(t *testing.T) {
	mock := &mockLLM{err: errors.New("api down")}
	_, err := SummarizeCluster(context.Background(), mock, []storage.KnowledgeEntry{{Title: "T", Content: "C"}})
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestScoreEntry(t *testing.T) {
	mock := &mockLLM{response: `{"coherence":0.8,"specificity":0.7}`}
	entry := storage.KnowledgeEntry{Title: "Test", Content: "Content"}
	score, err := ScoreEntry(context.Background(), mock, entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score.Coherence != 0.8 {
		t.Errorf("coherence = %v, want 0.8", score.Coherence)
	}
	if score.Specificity != 0.7 {
		t.Errorf("specificity = %v, want 0.7", score.Specificity)
	}
	const wantTotal = 1.5
	if score.Total != wantTotal {
		t.Errorf("total = %v, want %v", score.Total, wantTotal)
	}
}

func TestDetectGaps(t *testing.T) {
	mock := &mockLLM{response: `{"gaps":[{"domain":"risk","description":"Thin","entry_count":1,"recommendation":"Add more"}]}`}
	gaps, err := DetectGaps(context.Background(), mock, map[string]int{"finance": 10, "risk": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(gaps))
	}
	if gaps[0].Domain != "risk" {
		t.Errorf("domain = %q, want risk", gaps[0].Domain)
	}
	if gaps[0].Recommendation == "" {
		t.Error("recommendation is empty")
	}
}

func TestDetectGaps_Empty(t *testing.T) {
	mock := &mockLLM{response: `{"gaps":[]}`}
	gaps, err := DetectGaps(context.Background(), mock, map[string]int{"finance": 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gaps) != 0 {
		t.Errorf("expected 0 gaps, got %d", len(gaps))
	}
}

func TestExtractJSON_StripsFences(t *testing.T) {
	cases := []struct{ input, want string }{
		{`{"k":"v"}`, `{"k":"v"}`},
		{"```json\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
		{"```\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
	}
	for _, tc := range cases {
		if got := extractJSON(tc.input); got != tc.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short: got %q, want %q", got, "hello")
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long: got %q, want %q", got, "hello...")
	}
}

func TestSummarizeCluster_InvalidJSON(t *testing.T) {
	mock := &mockLLM{response: "not valid json"}
	_, err := SummarizeCluster(context.Background(), mock, []storage.KnowledgeEntry{{Title: "T", Content: "C"}})
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid json") {
		t.Errorf("error should contain original response, got: %v", err)
	}
}

func TestScoreEntry_InvalidJSON(t *testing.T) {
	mock := &mockLLM{response: "not valid json"}
	_, err := ScoreEntry(context.Background(), mock, storage.KnowledgeEntry{Title: "T", Content: "C"})
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid json") {
		t.Errorf("error should contain original response, got: %v", err)
	}
}

func TestDetectGaps_InvalidJSON(t *testing.T) {
	mock := &mockLLM{response: "not valid json"}
	_, err := DetectGaps(context.Background(), mock, map[string]int{"finance": 5})
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid json") {
		t.Errorf("error should contain original response, got: %v", err)
	}
}
