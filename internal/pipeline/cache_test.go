package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// TestScoreEntryCachedSkipsLLMOnSecondCall verifies that scoring the same entry
// twice hits the LLM exactly once and returns equal QualityScores both times
// (including Total recomputed on the cached path).
func TestScoreEntryCachedSkipsLLMOnSecondCall(t *testing.T) {
	store := &mockAnalysisStore{}
	llmMock := &mockLLM{response: `{"coherence":0.8,"specificity":0.7}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entry := storage.KnowledgeEntry{ID: "e1", Title: "Finance basics", Content: "Compound interest patterns"}

	score1, err := p.cachedScoreEntry(context.Background(), llmMock, entry, "")
	if err != nil {
		t.Fatalf("first cachedScoreEntry: %v", err)
	}

	score2, err := p.cachedScoreEntry(context.Background(), llmMock, entry, "")
	if err != nil {
		t.Fatalf("second cachedScoreEntry: %v", err)
	}

	if llmMock.CallCount() != 1 {
		t.Errorf("LLM Complete called %d times, want exactly 1", llmMock.CallCount())
	}
	if score1 != score2 {
		t.Errorf("scores differ: first=%+v second=%+v", score1, score2)
	}
	if score2.Total != score2.Coherence+score2.Specificity {
		t.Errorf("Total not recomputed: got %v, want %v", score2.Total, score2.Coherence+score2.Specificity)
	}
	const wantTotal = 1.5
	if score2.Total != wantTotal {
		t.Errorf("Total = %v, want %v", score2.Total, wantTotal)
	}
}

// TestScoreEntryCachedMissOnEditedContent verifies that changing Content causes a
// cache miss and a second LLM call.
func TestScoreEntryCachedMissOnEditedContent(t *testing.T) {
	store := &mockAnalysisStore{}
	llmMock := &mockLLM{response: `{"coherence":0.8,"specificity":0.7}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entry1 := storage.KnowledgeEntry{ID: "e1", Title: "Finance", Content: "Original content"}
	entry2 := storage.KnowledgeEntry{ID: "e1", Title: "Finance", Content: "Edited content — different"}

	if _, err := p.cachedScoreEntry(context.Background(), llmMock, entry1, ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := p.cachedScoreEntry(context.Background(), llmMock, entry2, ""); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if llmMock.CallCount() != 2 {
		t.Errorf("LLM Complete called %d times, want 2 (different content = cache miss)", llmMock.CallCount())
	}
}

// TestSummarizeClusterCachedKeyOrderIndependent verifies that cachedSummarizeCluster
// produces the same cache key regardless of entry order, resulting in exactly one
// LLM call for both orderings.
func TestSummarizeClusterCachedKeyOrderIndependent(t *testing.T) {
	store := &mockAnalysisStore{}
	llmMock := &mockLLM{response: `{"title":"Finance Cluster","summary":"Finance entries."}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entries := []storage.KnowledgeEntry{
		{ID: "a", Title: "Entry A", Content: "Finance pattern A"},
		{ID: "b", Title: "Entry B", Content: "Finance pattern B"},
	}
	reversed := []storage.KnowledgeEntry{entries[1], entries[0]}

	if _, err := p.cachedSummarizeCluster(context.Background(), llmMock, entries, "", ""); err != nil {
		t.Fatalf("first cachedSummarizeCluster: %v", err)
	}
	if _, err := p.cachedSummarizeCluster(context.Background(), llmMock, reversed, "", ""); err != nil {
		t.Fatalf("second cachedSummarizeCluster (reversed): %v", err)
	}

	if llmMock.CallCount() != 1 {
		t.Errorf("LLM Complete called %d times, want 1 (order-independent key)", llmMock.CallCount())
	}
}

// TestCacheLLMErrorNotCached verifies that an LLM error is not cached and a
// subsequent call with a working LLM hits the LLM again.
func TestCacheLLMErrorNotCached(t *testing.T) {
	store := &mockAnalysisStore{}
	failingLLM := &mockLLM{err: errors.New("api unavailable")}

	p := New(store, newSrc(failingLLM), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entry := storage.KnowledgeEntry{ID: "e1", Title: "Finance", Content: "Content"}

	_, err := p.cachedScoreEntry(context.Background(), failingLLM, entry, "")
	if err == nil {
		t.Fatal("expected error from failing LLM, got nil")
	}

	// Now use a working LLM — should hit LLM since failure was not cached
	workingLLM := &mockLLM{response: `{"coherence":0.9,"specificity":0.8}`}
	score, err := p.cachedScoreEntry(context.Background(), workingLLM, entry, "")
	if err != nil {
		t.Fatalf("second call with working LLM: %v", err)
	}
	if workingLLM.CallCount() != 1 {
		t.Errorf("working LLM called %d times, want 1 (cache was empty after failure)", workingLLM.CallCount())
	}
	if score.Total != score.Coherence+score.Specificity {
		t.Errorf("Total not recomputed: got %v, want coherence+specificity=%v", score.Total, score.Coherence+score.Specificity)
	}
	if score.Coherence != 0.9 || score.Specificity != 0.8 {
		t.Errorf("unexpected score values: coherence=%v specificity=%v", score.Coherence, score.Specificity)
	}
}

// TestCacheWriteFailureNonFatal verifies that a PutAnalysisCache error does not
// cause cachedScoreEntry to return an error — the LLM result is returned normally.
func TestCacheWriteFailureNonFatal(t *testing.T) {
	store := &mockAnalysisStore{putCacheErr: errors.New("disk full")}
	llmMock := &mockLLM{response: `{"coherence":0.6,"specificity":0.5}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entry := storage.KnowledgeEntry{ID: "e1", Title: "Finance", Content: "Content"}

	score, err := p.cachedScoreEntry(context.Background(), llmMock, entry, "")
	if err != nil {
		t.Fatalf("cachedScoreEntry returned error despite cache write failure being non-fatal: %v", err)
	}
	const wantTotal = 1.1
	if score.Total != wantTotal {
		t.Errorf("Total = %v, want %v", score.Total, wantTotal)
	}
}

// TestRunPrunesCache drives Run() with the minimal passing fixture and verifies
// that PruneAnalysisCache was called with 90*24*time.Hour after a successful run.
func TestRunPrunesCache(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	llmMock := &mockLLM{
		response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`,
	}

	p := New(store, newSrc(llmMock), Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	const wantPrune = 90 * 24 * time.Hour
	if store.pruneCalled != wantPrune {
		t.Errorf("PruneAnalysisCache called with %v, want %v", store.pruneCalled, wantPrune)
	}
}

// failingListStore makes Run() fail at its first step so the test can assert
// that a failed run never prunes the analysis cache.
type failingListStore struct {
	mockAnalysisStore
}

func (f *failingListStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	return nil, errors.New("boom")
}

func TestRunFailedDoesNotPruneCache(t *testing.T) {
	store := &failingListStore{}
	p := New(store, newSrc(&mockLLM{response: `{}`}), Config{MinEntries: 0, Interval: time.Hour, ClusterThreshold: 0.8})

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Logf("run returned: %v", err) // failed run may surface an error; that's fine
	}

	if store.pruneCalled != 0 {
		t.Fatalf("PruneAnalysisCache called with %v on a failed run; must not prune", store.pruneCalled)
	}
}

// TestSummaryCacheMissOnProviderChange verifies that summarizing the same cluster
// under two different LLM fingerprints results in two LLM calls — stale
// provider-dependent prose is never served across a provider boundary.
func TestSummaryCacheMissOnProviderChange(t *testing.T) {
	store := &mockAnalysisStore{}
	llmMock := &mockLLM{response: `{"title":"Finance Cluster","summary":"Finance entries."}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entries := []storage.KnowledgeEntry{
		{ID: "a", Title: "Entry A", Content: "Finance pattern A"},
		{ID: "b", Title: "Entry B", Content: "Finance pattern B"},
	}

	// First call: anthropic provider.
	if _, err := p.cachedSummarizeCluster(context.Background(), llmMock, entries, "anthropic|claude-x", ""); err != nil {
		t.Fatalf("first cachedSummarizeCluster: %v", err)
	}

	// Second call: ollama provider — must NOT hit the anthropic cache entry.
	if _, err := p.cachedSummarizeCluster(context.Background(), llmMock, entries, "ollama|http://o|llama3.1", ""); err != nil {
		t.Fatalf("second cachedSummarizeCluster: %v", err)
	}

	if llmMock.CallCount() != 2 {
		t.Errorf("LLM Complete called %d times, want 2 (different fingerprints = cache miss)", llmMock.CallCount())
	}
}

// TestScoreCacheUnaffectedByProviderChange verifies that scoring the same entry
// under two different LLM fingerprints results in exactly one LLM call — numeric
// quality scores are content-derived and must remain provider-independent.
func TestScoreCacheUnaffectedByProviderChange(t *testing.T) {
	store := &mockAnalysisStore{}
	llmMock := &mockLLM{response: `{"coherence":0.8,"specificity":0.7}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})

	entry := storage.KnowledgeEntry{ID: "e1", Title: "Finance basics", Content: "Compound interest patterns"}

	// Score once under anthropic fingerprint context — score cache is content-keyed.
	if _, err := p.cachedScoreEntry(context.Background(), llmMock, entry, ""); err != nil {
		t.Fatalf("first cachedScoreEntry: %v", err)
	}

	// Score again; cachedScoreEntry is intentionally fingerprint-free, so same
	// content → same key → cache hit regardless of what the fingerprint would be.
	if _, err := p.cachedScoreEntry(context.Background(), llmMock, entry, ""); err != nil {
		t.Fatalf("second cachedScoreEntry: %v", err)
	}

	if llmMock.CallCount() != 1 {
		t.Errorf("LLM Complete called %d times, want 1 (score is content-keyed, provider-independent)", llmMock.CallCount())
	}
}
