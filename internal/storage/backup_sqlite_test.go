package storage

import (
	"context"
	"testing"
)

func newTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(t.TempDir()+"/test.db", 4)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteDumpLoadTableRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "T1", Content: "C1", Tags: []string{"a", "b"}}, []float32{1, 2, 3, 4}); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	var rows []map[string]any
	if err := s.DumpTable(ctx, "entries", func(r map[string]any) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatalf("DumpTable: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	// tags is a TEXT column — must be a string, not []byte.
	if _, ok := rows[0]["tags"].(string); !ok {
		t.Fatalf("tags should be string, got %T", rows[0]["tags"])
	}

	dst := newTestSQLite(t)
	if err := dst.LoadTable(ctx, "entries", rows); err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	got, err := dst.GetEntry(ctx, rows[0]["id"].(string))
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Title != "T1" {
		t.Errorf("title = %q, want T1", got.Title)
	}
}

func TestSQLiteEmbeddingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	id, _ := s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "T", Content: "C"}, []float32{1, 2, 3, 4})

	var items []EmbeddingItem
	if err := s.DumpEmbeddings(ctx, func(it EmbeddingItem) error {
		items = append(items, it)
		return nil
	}); err != nil {
		t.Fatalf("DumpEmbeddings: %v", err)
	}
	if len(items) != 1 || items[0].EntryID != id || len(items[0].Vector) != 4 {
		t.Fatalf("unexpected embeddings: %+v", items)
	}

	dst := newTestSQLite(t)
	dst.LoadTable(ctx, "entries", dumpAll(t, s, "entries"))
	if err := dst.LoadEmbeddings(ctx, items); err != nil {
		t.Fatalf("LoadEmbeddings: %v", err)
	}
	// vec_entries must be rebuilt: a similarity search should return the entry.
	res, err := dst.SearchSimilar(ctx, []float32{1, 2, 3, 4}, 1)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(res))
	}
}

func TestSQLiteIsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLite(t)
	empty, err := s.IsEmpty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Error("fresh store should be empty")
	}
	s.StoreEntry(ctx, KnowledgeEntry{Type: "note", Title: "X", Content: "Y"}, nil)
	empty, _ = s.IsEmpty(ctx)
	if empty {
		t.Error("store with an entry should not be empty")
	}
}

// dumpAll is a tiny test helper.
func dumpAll(t *testing.T, s BackupStore, table string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := s.DumpTable(context.Background(), table, func(r map[string]any) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatalf("dumpAll %s: %v", table, err)
	}
	return rows
}
