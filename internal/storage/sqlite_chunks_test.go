package storage_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// newTestSQLiteStore opens a temp SQLite DB with embeddingDim=4 and also returns
// the concrete *SQLiteStore so tests can issue raw SQL count queries.
func newTestChunkStore(t *testing.T) (*storage.SQLiteStore, *sql.DB) {
	t.Helper()
	f, err := os.CreateTemp("", "knowledge-chunk-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })

	store, err := storage.NewSQLiteStore(path, 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Separate connection for raw assertions against the same DB file.
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return store, db
}

func chunkSampleEntry() storage.KnowledgeEntry {
	return storage.KnowledgeEntry{
		Type:    storage.KTPrompt,
		Title:   "Chunked Prompt",
		Content: "chunk zero content",
		Domain:  "finance",
		Tags:    []string{"a"},
		Author:  "alice",
		Team:    "analysts",
	}
}

func countChunks(t *testing.T, db *sql.DB, entryID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entry_chunks WHERE entry_id=?`, entryID).Scan(&n); err != nil {
		t.Fatalf("count entry_chunks: %v", err)
	}
	return n
}

func countVecChunks(t *testing.T, db *sql.DB, entryID string) int {
	t.Helper()
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM vec_chunks
		WHERE rowid IN (SELECT rowid FROM entry_chunks WHERE entry_id=?)`, entryID).Scan(&n)
	if err != nil {
		t.Fatalf("count vec_chunks: %v", err)
	}
	return n
}

func TestStoreEntryChunked_MultipleChunksOneEntry(t *testing.T) {
	ctx := context.Background()
	store, db := newTestChunkStore(t)

	chunks := []storage.EntryChunk{
		{Index: 0, Content: "rep chunk", TokenEstimate: 2, Embedding: []float32{1, 0, 0, 0}},
		{Index: 1, Content: "second chunk", TokenEstimate: 2, Embedding: []float32{0, 1, 0, 0}},
		{Index: 2, Content: "third chunk", TokenEstimate: 2, Embedding: []float32{0, 0, 1, 0}},
	}

	id, err := store.StoreEntryChunked(ctx, chunkSampleEntry(), chunks)
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}

	got, err := store.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.ID != id {
		t.Fatalf("GetEntry id mismatch: %q vs %q", got.ID, id)
	}

	// Exactly one logical entry.
	entries, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	if n := countChunks(t, db, id); n != 3 {
		t.Errorf("entry_chunks count = %d, want 3", n)
	}
	if n := countVecChunks(t, db, id); n != 3 {
		t.Errorf("vec_chunks count = %d, want 3", n)
	}
}

func TestSearchSimilar_DedupesToEntry(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestChunkStore(t)

	query := []float32{0, 1, 0, 0}
	chunks := []storage.EntryChunk{
		{Index: 0, Content: "rep", Embedding: []float32{1, 0, 0, 0}},
		{Index: 1, Content: "match", Embedding: query}, // equals query
		{Index: 2, Content: "other", Embedding: []float32{0, 0, 1, 0}},
	}
	id, err := store.StoreEntryChunked(ctx, chunkSampleEntry(), chunks)
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}

	results, err := store.SearchSimilar(ctx, query, 5)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}

	count := 0
	for _, r := range results {
		if r.Entry.ID == id {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("entry appeared %d times in results, want exactly 1", count)
	}
}

func TestStoreEntry_BackwardCompatSingleChunk(t *testing.T) {
	ctx := context.Background()
	store, db := newTestChunkStore(t)

	emb := []float32{0.5, 0.5, 0, 0}
	id, err := store.StoreEntry(ctx, chunkSampleEntry(), emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	if n := countChunks(t, db, id); n != 1 {
		t.Errorf("entry_chunks count = %d, want 1", n)
	}

	results, err := store.SearchSimilar(ctx, emb, 5)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Entry.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("legacy single-chunk entry not found by search")
	}
}

func TestReplaceEntryChunks(t *testing.T) {
	ctx := context.Background()
	store, db := newTestChunkStore(t)

	oldVec := []float32{1, 0, 0, 0}
	id, err := store.StoreEntry(ctx, chunkSampleEntry(), oldVec)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	newVecA := []float32{0, 1, 0, 0}
	newVecB := []float32{0, 0, 1, 0}
	newChunks := []storage.EntryChunk{
		{Index: 0, Content: "new rep", Embedding: newVecA},
		{Index: 1, Content: "new second", Embedding: newVecB},
	}
	if err := store.ReplaceEntryChunks(ctx, id, newChunks); err != nil {
		t.Fatalf("ReplaceEntryChunks: %v", err)
	}

	if n := countChunks(t, db, id); n != 2 {
		t.Errorf("entry_chunks count after replace = %d, want 2", n)
	}

	// New vector should now be findable.
	res, err := store.SearchSimilar(ctx, newVecB, 5)
	if err != nil {
		t.Fatalf("SearchSimilar new: %v", err)
	}
	foundNew := false
	for _, r := range res {
		if r.Entry.ID == id {
			foundNew = true
		}
	}
	if !foundNew {
		t.Error("new chunk vector not found after replace")
	}

	// Old representative vector should no longer match this entry as a near-zero
	// distance result (it was replaced). Query with oldVec; the best distance to
	// this entry should be >0 since no chunk equals oldVec anymore.
	res2, err := store.SearchSimilar(ctx, oldVec, 5)
	if err != nil {
		t.Fatalf("SearchSimilar old: %v", err)
	}
	for _, r := range res2 {
		if r.Entry.ID == id && r.Score == 0 {
			t.Error("replaced-away vector still produces an exact (0-distance) match")
		}
	}
}

func TestDeleteEntry_RemovesChunks(t *testing.T) {
	ctx := context.Background()
	store, db := newTestChunkStore(t)

	chunks := []storage.EntryChunk{
		{Index: 0, Content: "rep", Embedding: []float32{1, 0, 0, 0}},
		{Index: 1, Content: "second", Embedding: []float32{0, 1, 0, 0}},
	}
	id, err := store.StoreEntryChunked(ctx, chunkSampleEntry(), chunks)
	if err != nil {
		t.Fatalf("StoreEntryChunked: %v", err)
	}

	if err := store.DeleteEntry(ctx, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	if n := countChunks(t, db, id); n != 0 {
		t.Errorf("entry_chunks remaining = %d, want 0", n)
	}
	if n := countVecChunks(t, db, id); n != 0 {
		t.Errorf("vec_chunks remaining = %d, want 0", n)
	}
}
