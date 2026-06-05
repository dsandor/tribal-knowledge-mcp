package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// newTestStore opens a temp SQLite DB with embeddingDim=4 for fast test vectors.
func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	f, err := os.CreateTemp("", "knowledge-test-*.db")
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
	return store
}

func sampleEntry() storage.KnowledgeEntry {
	return storage.KnowledgeEntry{
		Type:        storage.KTPrompt,
		Title:       "Earnings Summary Prompt",
		Content:     "Summarize the earnings call transcript focusing on guidance and surprises.",
		Description: "Use for Q earnings summaries",
		Domain:      "finance",
		Tags:        []string{"earnings", "summary"},
		Author:      "alice",
		Team:        "analysts",
	}
}

func TestStoreAndGet(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	entry := sampleEntry()
	emb := []float32{0.1, 0.2, 0.3, 0.4}

	id, err := store.StoreEntry(ctx, entry, emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if id == "" {
		t.Fatal("StoreEntry returned empty ID")
	}

	entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	got, err := store.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Title != entry.Title {
		t.Errorf("Title: got %q, want %q", got.Title, entry.Title)
	}
	if got.Domain != "finance" {
		t.Errorf("Domain: got %q, want %q", got.Domain, "finance")
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags: got %v, want 2 tags", got.Tags)
	}
	if got.Version != 1 {
		t.Errorf("Version: got %d, want 1", got.Version)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestGetEntry_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	_, err := store.GetEntry(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for missing entry, got nil")
	}
}

func TestDeleteEntry(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}
	id, err := store.StoreEntry(ctx, sampleEntry(), emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	if err := store.DeleteEntry(ctx, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	remaining, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if len(remaining) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(remaining))
	}
}

func TestListEntries_DomainFilter(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}

	financeEntry := sampleEntry()
	financeEntry.Domain = "finance"
	techEntry := sampleEntry()
	techEntry.Domain = "tech"

	_, _ = store.StoreEntry(ctx, financeEntry, emb)
	_, _ = store.StoreEntry(ctx, techEntry, emb)

	all, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	if len(all) != 2 {
		t.Fatalf("want 2 total, got %d", len(all))
	}

	filtered, _ := store.ListEntries(ctx, storage.ListFilter{Domain: "finance", Limit: 10})
	if len(filtered) != 1 {
		t.Fatalf("want 1 finance entry, got %d", len(filtered))
	}
	if filtered[0].Domain != "finance" {
		t.Errorf("Domain: got %q, want finance", filtered[0].Domain)
	}
}

func TestSearchSimilar(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Three orthogonal-ish 4-dim vectors
	north := []float32{1.0, 0.0, 0.0, 0.0}
	east := []float32{0.0, 1.0, 0.0, 0.0}
	nearNorth := []float32{0.9, 0.1, 0.0, 0.0}

	e1 := sampleEntry()
	e1.Title = "North"
	e2 := sampleEntry()
	e2.Title = "East"
	e3 := sampleEntry()
	e3.Title = "NearNorth"

	_, _ = store.StoreEntry(ctx, e1, north)
	_, _ = store.StoreEntry(ctx, e2, east)
	_, _ = store.StoreEntry(ctx, e3, nearNorth)

	query := []float32{0.95, 0.05, 0.0, 0.0}
	results, err := store.SearchSimilar(ctx, query, 2)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	// East is orthogonal to query — must not appear in top-2
	for _, r := range results {
		if r.Entry.Title == "East" {
			t.Error("East should not appear in top-2 results for a northward query")
		}
	}
}

func TestEntryTimestamps(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	before := time.Now().UTC().Truncate(time.Second)
	_, _ = store.StoreEntry(ctx, sampleEntry(), []float32{0.1, 0.2, 0.3, 0.4})
	after := time.Now().UTC().Add(5 * time.Second)

	entries, _ := store.ListEntries(ctx, storage.ListFilter{Limit: 10})
	ts := entries[0].CreatedAt.UTC()
	if ts.Before(before) || ts.After(after) {
		t.Errorf("CreatedAt %v outside expected range [%v, %v]", ts, before, after)
	}
}
