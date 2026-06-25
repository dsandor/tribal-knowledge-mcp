package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// errEmbedder always fails to embed, simulating an unreachable Ollama/OpenAI
// backend so we can verify embedding failures are non-fatal on write paths.
type errEmbedder struct{}

func (errEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embedder unreachable")
}

// errEmbedProvider always hands back the failing embedder.
type errEmbedProvider struct{}

func (errEmbedProvider) Embedder(_ string, _ string) embedding.Embedder { return errEmbedder{} }

func (errEmbedProvider) OpenAIEmbedder(_, _, _ string) embedding.Embedder { return errEmbedder{} }

func newErrEmbedSources(ec aiconfig.EmbedConfigStore) *aiconfig.Sources {
	r := aiconfig.NewResolver(reembedSettingsStore{}, aiconfig.EnvDefaults{})
	return &aiconfig.Sources{
		Resolver:    r,
		Embed:       errEmbedProvider{},
		EmbedConfig: ec,
		DefaultTeam: "test-team",
	}
}

func newErrEmbedServer(t *testing.T, store storage.SQLiteStore) *web.Server {
	t.Helper()
	srv := newReembedServer(t, store)
	return srv.WithAISources(newErrEmbedSources(&store))
}

// TestKnowledgeUpdate_EmbedErrorIsFailSoft verifies that editing an entry's
// content returns 200 (not 500) when the embedder errors, and that the new
// text is persisted even though the vectors could not be refreshed.
func TestKnowledgeUpdate_EmbedErrorIsFailSoft(t *testing.T) {
	ctx := context.Background()
	store, _ := newReembedStore(t)

	const contentA = "alpha alpha alpha original content about apples"
	const contentB = "bravo bravo bravo completely different text about zebras"

	entry := storage.KnowledgeEntry{
		Type:    storage.KTPrompt,
		Title:   "Editable",
		Content: contentA,
		Domain:  "finance",
		Status:  "approved",
		TeamID:  "test-team",
	}
	id, err := store.StoreEntryChunked(ctx, entry, []storage.EntryChunk{
		{Index: 0, Content: contentA, TokenEstimate: 1, Embedding: vecForText(contentA)},
	})
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}

	srv := newErrEmbedServer(t, *store)

	body, _ := json.Marshal(map[string]any{"content": contentB})
	req := httptest.NewRequest("PUT", "/api/knowledge/"+id, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update with failing embedder: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%s)", err, w.Body.String())
	}
	if resp["ok"] != true {
		t.Fatalf("expected ok=true, got %v", resp)
	}
	if resp["embedding_skipped"] != true {
		t.Fatalf("expected embedding_skipped=true, got %v", resp)
	}

	// The entry text must reflect the edit even though embedding failed.
	got, err := store.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Content != contentB {
		t.Fatalf("entry content = %q, want %q (edit must persist despite embed failure)", got.Content, contentB)
	}
}

// TestKnowledgeUpdate_NilEmbedderIsFailSoft verifies that a content edit returns
// 200 and persists when no embedder is configured at all (aiSrc embedder nil).
func TestKnowledgeUpdate_NilEmbedderIsFailSoft(t *testing.T) {
	ctx := context.Background()
	store, _ := newReembedStore(t)

	const contentA = "alpha original"
	const contentB = "bravo updated"

	entry := storage.KnowledgeEntry{
		Type:    storage.KTPrompt,
		Title:   "Editable",
		Content: contentA,
		Status:  "approved",
		TeamID:  "test-team",
	}
	id, err := store.StoreEntryChunked(ctx, entry, []storage.EntryChunk{
		{Index: 0, Content: contentA, TokenEstimate: 1, Embedding: vecForText(contentA)},
	})
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}

	srv := newNilEmbedServer(t, *store)

	body, _ := json.Marshal(map[string]any{"content": contentB})
	req := httptest.NewRequest("PUT", "/api/knowledge/"+id, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update with nil embedder: want 200, got %d: %s", w.Code, w.Body.String())
	}

	got, err := store.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Content != contentB {
		t.Fatalf("entry content = %q, want %q", got.Content, contentB)
	}
}

// nilEmbedProvider returns a nil embedder, simulating an unconfigured backend.
type nilEmbedProvider struct{}

func (nilEmbedProvider) Embedder(_ string, _ string) embedding.Embedder { return nil }

func (nilEmbedProvider) OpenAIEmbedder(_, _, _ string) embedding.Embedder { return nil }

func newNilEmbedServer(t *testing.T, store storage.SQLiteStore) *web.Server {
	t.Helper()
	r := aiconfig.NewResolver(reembedSettingsStore{}, aiconfig.EnvDefaults{})
	src := &aiconfig.Sources{Resolver: r, Embed: nilEmbedProvider{}, EmbedConfig: &store, DefaultTeam: "test-team"}
	return newReembedServer(t, store).WithAISources(src)
}
