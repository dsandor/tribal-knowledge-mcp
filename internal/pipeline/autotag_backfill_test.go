package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// trackingStore wraps mockAnalysisStore and records UpdateAutoTags call IDs.
type trackingStore struct {
	mockAnalysisStore
	updatedIDs []string
}

func (s *trackingStore) UpdateAutoTags(_ context.Context, id string, _ []string) error {
	s.updatedIDs = append(s.updatedIDs, id)
	return nil
}

// TestAutoTagBackfill verifies that runAutoTagBackfill calls UpdateAutoTags only
// for entries whose AutoTags are empty, skipping entries that already have tags.
func TestAutoTagBackfill(t *testing.T) {
	store := &trackingStore{
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "a", Title: "Entry A", Content: "Alpha content", Domain: "finance", AutoTags: nil},
				{ID: "b", Title: "Entry B", Content: "Beta content", Domain: "finance", AutoTags: []string{"done"}},
			},
		},
	}

	// Fake LLM returns tags JSON.
	llmMock := &mockLLM{response: `{"tags":["x","y"]}`}
	src := &mockAISource{improvementClient: llmMock}

	p := New(store, src, Config{
		MinEntries: 1,
		Interval:   time.Hour,
	})

	p.runAutoTagBackfill(context.Background(), llmMock, "team1")

	if len(store.updatedIDs) != 1 {
		t.Fatalf("want 1 UpdateAutoTags call (entry A only), got %d: %v", len(store.updatedIDs), store.updatedIDs)
	}
	if store.updatedIDs[0] != "a" {
		t.Errorf("expected UpdateAutoTags called for entry 'a', got %q", store.updatedIDs[0])
	}
}

// TestAutoTagBackfillCap verifies that at most autoTagBackfillCap (20) entries
// are tagged per run, even when more untagged entries exist.
func TestAutoTagBackfillCap(t *testing.T) {
	entries := make([]storage.KnowledgeEntry, 25)
	for i := range entries {
		entries[i] = storage.KnowledgeEntry{
			ID:      string(rune('a' + i)),
			Title:   "Entry",
			Content: "Content",
			Domain:  "finance",
		}
	}

	store := &trackingStore{
		mockAnalysisStore: mockAnalysisStore{entries: entries},
	}

	llmMock := &mockLLM{response: `{"tags":["x","y"]}`}

	p := New(store, &mockAISource{improvementClient: llmMock}, Config{
		MinEntries: 1,
		Interval:   time.Hour,
	})

	p.runAutoTagBackfill(context.Background(), llmMock, "team1")

	if len(store.updatedIDs) != autoTagBackfillCap {
		t.Errorf("UpdateAutoTags called %d times, want exactly %d (cap)", len(store.updatedIDs), autoTagBackfillCap)
	}
}
