package backup_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/backup"
	"github.com/dsandor/memory/internal/storage"
)

func newSQLite(t *testing.T) *storage.SQLiteStore {
	t.Helper()
	s, err := storage.NewSQLiteStore(t.TempDir()+"/db.sqlite", 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteFullRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := newSQLite(t)
	id, err := src.StoreEntry(ctx, storage.KnowledgeEntry{
		Type: "note", Title: "Round", Content: "Trip", Tags: []string{"x", "y"},
	}, []float32{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0)); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst := newSQLite(t)
	if _, err := backup.Import(ctx, dst, &buf, backup.ImportOptions{}, 4); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := dst.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get restored entry: %v", err)
	}
	if got.Title != "Round" || len(got.Tags) != 2 {
		t.Errorf("restored entry wrong: %+v", got)
	}
	res, err := dst.SearchSimilar(ctx, []float32{1, 2, 3, 4}, 1)
	if err != nil || len(res) != 1 {
		t.Fatalf("vector search after restore failed: res=%d err=%v", len(res), err)
	}
}

func TestForceOverwrites(t *testing.T) {
	ctx := context.Background()
	src := newSQLite(t)
	src.StoreEntry(ctx, storage.KnowledgeEntry{Type: "note", Title: "A", Content: "a"}, nil)
	var buf bytes.Buffer
	backup.Export(ctx, src, &buf, "test", 4, time.Unix(0, 0))

	dst := newSQLite(t)
	dst.StoreEntry(ctx, storage.KnowledgeEntry{Type: "note", Title: "PreExisting", Content: "z"}, nil)

	if _, err := backup.Import(ctx, dst, bytes.NewReader(buf.Bytes()), backup.ImportOptions{}, 4); err == nil {
		t.Fatal("expected refusal without force")
	}
	if _, err := backup.Import(ctx, dst, bytes.NewReader(buf.Bytes()), backup.ImportOptions{Force: true}, 4); err != nil {
		t.Fatalf("force import: %v", err)
	}
	entries, _ := dst.ListEntries(ctx, storage.ListFilter{})
	if len(entries) != 1 || entries[0].Title != "A" {
		t.Errorf("after force restore expected only 'A', got %+v", entries)
	}
}
