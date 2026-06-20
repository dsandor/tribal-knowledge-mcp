package storage

import (
	"testing"
)

// Compile-time assertions that both stores implement BackupStore.
var (
	_ BackupStore = (*SQLiteStore)(nil)
	_ BackupStore = (*PostgresStore)(nil)
)

func TestEmbeddingItemRoundTripsType(t *testing.T) {
	item := EmbeddingItem{EntryID: "abc", Vector: []float32{1, 2, 3}}
	if item.EntryID != "abc" || len(item.Vector) != 3 {
		t.Fatal("EmbeddingItem fields wrong")
	}
}
