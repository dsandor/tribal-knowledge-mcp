package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// TestRebuildEmbeddingColumns verifies that RebuildEmbeddingColumns changes the
// effective embedding dimension: after rebuilding to dim 8, an 8-length vector
// is accepted and a 4-length vector is rejected with a dim-mismatch error.
func TestRebuildEmbeddingColumns(t *testing.T) {
	ctx := context.Background()
	store, db := newTestChunkStore(t) // dim 4

	// Store an entry at the original dimension (4) to prove the table existed
	// and accepted dim-4 vectors before the rebuild.
	id, err := store.StoreEntryChunked(ctx, chunkSampleEntry(), []storage.EntryChunk{
		{Index: 0, Content: "original chunk", TokenEstimate: 2, Embedding: []float32{1, 0, 0, 0}},
	})
	if err != nil {
		t.Fatalf("StoreEntryChunked (dim 4): %v", err)
	}
	if n := countChunks(t, db, id); n != 1 {
		t.Fatalf("want 1 chunk before rebuild, got %d", n)
	}

	// Rebuild the vector storage at the new dimension.
	if err := store.RebuildEmbeddingColumns(ctx, 8); err != nil {
		t.Fatalf("RebuildEmbeddingColumns(8): %v", err)
	}

	// A dim-8 vector must now succeed.
	if err := store.ReplaceEntryChunks(ctx, id, []storage.EntryChunk{
		{Index: 0, Content: "new chunk", TokenEstimate: 2, Embedding: []float32{1, 0, 0, 0, 0, 0, 0, 0}},
	}); err != nil {
		t.Fatalf("ReplaceEntryChunks (dim 8) should succeed after rebuild: %v", err)
	}
	if n := countVecChunks(t, db, id); n != 1 {
		t.Fatalf("want 1 vec_chunk after dim-8 replace, got %d", n)
	}

	// A dim-4 vector must now fail the dimension check.
	err = store.ReplaceEntryChunks(ctx, id, []storage.EntryChunk{
		{Index: 0, Content: "stale chunk", TokenEstimate: 2, Embedding: []float32{1, 0, 0, 0}},
	})
	if err == nil {
		t.Fatalf("ReplaceEntryChunks (dim 4) should fail after rebuild, got nil error")
	}
	if !strings.Contains(err.Error(), "embedding dim mismatch") {
		t.Fatalf("want dim-mismatch error, got: %v", err)
	}

	// StoreEntryChunked for a brand-new entry must also enforce the new dim.
	if _, err := store.StoreEntryChunked(ctx, chunkSampleEntry(), []storage.EntryChunk{
		{Index: 0, Content: "new entry chunk", TokenEstimate: 2, Embedding: []float32{1, 0, 0, 0, 0, 0, 0, 0}},
	}); err != nil {
		t.Fatalf("StoreEntryChunked (dim 8) should succeed after rebuild: %v", err)
	}
}
