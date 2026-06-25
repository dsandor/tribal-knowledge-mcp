package web_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"io/fs"
	"testing/fstest"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// reembedDim is the embedding dimension used by the re-embed test store.
const reembedDim = 4

// stubEmbedder returns a deterministic vector per input string. Identical
// inputs always map to the same vector, and different inputs map to clearly
// different vectors, so a SearchSimilar with a query built from the same text
// matches exactly.
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return vecForText(text), nil
}

// vecForText derives a stable unit-ish vector from text. Content "A" and
// content "B" produce distinctly different vectors so similarity search can
// tell them apart.
func vecForText(text string) []float32 {
	v := make([]float32, reembedDim)
	for i, r := range text {
		v[i%reembedDim] += float32(r)
	}
	// Add a length signal so empty/short strings still differ.
	v[utf8.RuneCountInString(text)%reembedDim] += 1
	return v
}

// stubEmbedProvider satisfies aiconfig.EmbedProvider. It ignores url/model and
// always returns the deterministic stub embedder so the handler resolves a
// non-nil embedder regardless of saved settings.
type stubEmbedProvider struct{}

func (stubEmbedProvider) Embedder(_ string, _ string) embedding.Embedder { return stubEmbedder{} }

func (stubEmbedProvider) OpenAIEmbedder(_, _, _ string) embedding.Embedder { return stubEmbedder{} }

// reembedSettingsStore is a minimal aiconfig.SettingsStore returning empty
// settings (no Ollama URL needed because the stub provider ignores them).
type reembedSettingsStore struct{}

func (reembedSettingsStore) GetTeamSettings(_ context.Context, teamID string) (*storage.TeamSettings, error) {
	return &storage.TeamSettings{TeamID: teamID}, nil
}

func newReembedStore(t *testing.T) (*storage.SQLiteStore, *sql.DB) {
	t.Helper()
	f, err := os.CreateTemp("", "web-reembed-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })

	store, err := storage.NewSQLiteStore(path, reembedDim)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return store, db
}

func newReembedSources(ec aiconfig.EmbedConfigStore) *aiconfig.Sources {
	r := aiconfig.NewResolver(reembedSettingsStore{}, aiconfig.EnvDefaults{})
	return &aiconfig.Sources{
		Resolver:    r,
		Embed:       stubEmbedProvider{},
		EmbedConfig: ec,
		DefaultTeam: "test-team",
	}
}

func newReembedServer(t *testing.T, store storage.SQLiteStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	var fsys fs.FS = staticFS
	return web.NewServer(fsys, &store).
		WithDevBypass(true).
		WithAISources(newReembedSources(&store))
}

func chunkContents(t *testing.T, db *sql.DB, entryID string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT content FROM entry_chunks WHERE entry_id=? ORDER BY chunk_index`, entryID)
	if err != nil {
		t.Fatalf("query entry_chunks: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan chunk: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// TestKnowledgeUpdate_ReembedsContent verifies that editing an entry's content
// via PUT /api/knowledge/{id} refreshes the stored vectors and chunk rows, so
// similarity search matches the NEW content (and no longer the old vector).
func TestKnowledgeUpdate_ReembedsContent(t *testing.T) {
	ctx := context.Background()
	store, db := newReembedStore(t)

	const contentA = "alpha alpha alpha original content about apples"
	const contentB = "bravo bravo bravo completely different text about zebras"

	// Store content A with its embedding (single representative chunk).
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

	srv := newReembedServer(t, *store)

	// Update content A -> B.
	body, _ := json.Marshal(map[string]any{"content": contentB})
	req := httptest.NewRequest("PUT", "/api/knowledge/"+id, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d: %s", w.Code, w.Body.String())
	}

	// The stored chunk rows must now reflect B, not A.
	chunks := chunkContents(t, db, id)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk after update, got %d: %v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0], "bravo") || strings.Contains(chunks[0], "alpha") {
		t.Fatalf("chunk content not refreshed to B: %q", chunks[0])
	}

	// SearchSimilar with B's query vector must return our entry as top hit.
	resB, err := store.SearchSimilar(ctx, vecForText(contentB), 5)
	if err != nil {
		t.Fatalf("SearchSimilar(B): %v", err)
	}
	if len(resB) == 0 || resB[0].Entry.ID != id {
		t.Fatalf("search with B vector did not return updated entry; got %+v", resB)
	}
	if resB[0].Entry.Content != contentB {
		t.Fatalf("returned entry content = %q, want B", resB[0].Entry.Content)
	}
}
