package sharing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/sharing"
	"github.com/dsandor/memory/internal/storage"
)

// --- test embedder ---

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3, 0.4}, nil
}

// --- fake AI providers for constructing *aiconfig.Sources ---

type fakeEmbedProvider struct{ e embedding.Embedder }

func (f *fakeEmbedProvider) Embedder(_, _ string) embedding.Embedder { return f.e }

type fakeLLMProvider struct{}

func (fakeLLMProvider) Client(_, _ string) llm.Client { return nil }
func (fakeLLMProvider) Ollama(_, _ string) llm.Client { return nil }

type fakeSettingsStore struct{}

func (fakeSettingsStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	return &storage.TeamSettings{}, nil
}

// newTestSources builds Sources whose embedder is non-nil for any team.
func newTestSources(e embedding.Embedder) *aiconfig.Sources {
	resolver := aiconfig.NewResolver(&fakeSettingsStore{}, aiconfig.EnvDefaults{
		AnthropicAPIKey: "test-key",
		AnthropicModel:  "test-model",
		OllamaURL:       "http://test-ollama",
		OllamaModel:     "test-model",
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         fakeLLMProvider{},
		Embed:       &fakeEmbedProvider{e: e},
		DefaultTeam: "default",
	}
}

// newNilEmbedSources builds Sources whose embedder is nil (unconfigured).
func newNilEmbedSources() *aiconfig.Sources {
	resolver := aiconfig.NewResolver(&fakeSettingsStore{}, aiconfig.EnvDefaults{
		AnthropicAPIKey: "test-key",
		AnthropicModel:  "test-model",
		OllamaURL:       "",
		OllamaModel:     "",
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         fakeLLMProvider{},
		Embed:       &fakeEmbedProvider{e: nil},
		DefaultTeam: "default",
	}
}

func newStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.NewSQLiteStore(t.TempDir()+"/test.db", 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// storeSourceEntry creates an entry in teamA and returns its id.
func storeSourceEntry(t *testing.T, store storage.Store) string {
	t.Helper()
	entry := storage.KnowledgeEntry{
		Type:        storage.KTPrompt,
		Title:       "Earnings Summary Prompt",
		Content:     "Summarize the earnings call focusing on guidance and margins.",
		Description: "A reliable earnings prompt.",
		Domain:      "finance",
		Tags:        []string{"earnings"},
		Author:      "alice",
		Team:        "Team A",
		TeamID:      "teamA",
		Status:      "approved",
	}
	chunks := []storage.EntryChunk{{Index: 0, Content: entry.Content, TokenEstimate: 10, Embedding: []float32{0.1, 0.2, 0.3, 0.4}}}
	id, err := store.StoreEntryChunked(context.Background(), entry, chunks)
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}
	return id
}

func TestImport_CreatesPendingEntryInDestTeam(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newTestSources(stubEmbedder{})

	entryID := storeSourceEntry(t, store)
	share, err := sharing.CreateShare(ctx, store, entryID, "teamA", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	newID, err := sharing.Import(ctx, store, src, share.ID, "teamB", "bob")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if newID == "" || newID == entryID {
		t.Fatalf("expected a fresh entry id, got %q (source %q)", newID, entryID)
	}

	got, err := store.GetEntry(ctx, newID)
	if err != nil {
		t.Fatalf("GetEntry(new): %v", err)
	}
	if got.TeamID != "teamB" {
		t.Errorf("TeamID = %q, want teamB", got.TeamID)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if got.Author != "alice" {
		t.Errorf("Author = %q, want preserved alice", got.Author)
	}
	if got.Title != "Earnings Summary Prompt" {
		t.Errorf("Title = %q, want preserved", got.Title)
	}

	// Searchable: the new entry has chunk vectors.
	results, err := store.SearchSimilar(ctx, []float32{0.1, 0.2, 0.3, 0.4}, 10)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Entry.ID == newID {
			found = true
		}
	}
	if !found {
		t.Errorf("imported entry %q not found in SearchSimilar results (no chunk vectors?)", newID)
	}
}

func TestImport_BurnsToken(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newTestSources(stubEmbedder{})

	entryID := storeSourceEntry(t, store)
	share, err := sharing.CreateShare(ctx, store, entryID, "teamA", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	if _, err := sharing.Import(ctx, store, src, share.ID, "teamB", "bob"); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	if _, err := sharing.Import(ctx, store, src, share.ID, "teamB", "carol"); err == nil {
		t.Fatalf("second Import succeeded; token should be single-use")
	}
}

func TestImport_SameTeam_ReturnsErrSameTeam(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newTestSources(stubEmbedder{})

	entryID := storeSourceEntry(t, store)
	share, err := sharing.CreateShare(ctx, store, entryID, "teamA", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	_, err = sharing.Import(ctx, store, src, share.ID, "teamA", "alice2")
	if !errors.Is(err, sharing.ErrSameTeam) {
		t.Fatalf("expected ErrSameTeam, got %v", err)
	}
}

func TestImport_RevokedShare_Errors(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newTestSources(stubEmbedder{})

	entryID := storeSourceEntry(t, store)
	share, err := sharing.CreateShare(ctx, store, entryID, "teamA", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if err := store.RevokeShare(ctx, share.ID); err != nil {
		t.Fatalf("RevokeShare: %v", err)
	}
	if _, err := sharing.Import(ctx, store, src, share.ID, "teamB", "bob"); err == nil {
		t.Fatalf("expected error importing revoked share")
	}
}

func TestImport_UnknownShare_Errors(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newTestSources(stubEmbedder{})

	if _, err := sharing.Import(ctx, store, src, "does-not-exist", "teamB", "bob"); err == nil {
		t.Fatalf("expected error for unknown share")
	}
}

func TestImport_NilEmbedder_Errors(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	src := newNilEmbedSources()

	entryID := storeSourceEntry(t, store)
	share, err := sharing.CreateShare(ctx, store, entryID, "teamA", "alice")
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if _, err := sharing.Import(ctx, store, src, share.ID, "teamB", "bob"); err == nil {
		t.Fatalf("expected error when embedding not configured")
	}
}
