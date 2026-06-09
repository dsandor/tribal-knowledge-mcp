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

// storeTestEntry stores an entry with the given title/content and returns its ID.
func storeTestEntry(t *testing.T, s storage.Store, title, content string) string {
	t.Helper()
	e := storage.KnowledgeEntry{
		Type:    storage.KTPrompt,
		Title:   title,
		Content: content,
		Domain:  "test",
		Tags:    []string{},
		Author:  "tester",
		Team:    "test-team",
	}
	id, err := s.StoreEntry(context.Background(), e, []float32{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatalf("storeTestEntry %q: %v", title, err)
	}
	return id
}

func TestRateEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := storeTestEntry(t, s, "Rate me", "rate content")

	if err := s.RateEntry(ctx, id, 4.5); err != nil {
		t.Fatalf("RateEntry: %v", err)
	}
	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if e.Rating != 4.5 {
		t.Errorf("Rating = %v, want 4.5", e.Rating)
	}
}

func TestRateEntry_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.RateEntry(context.Background(), "nonexistent-id", 3.0)
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestListEntries_OffsetAndSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, title := range []string{"Alpha analytics", "Beta reporting", "Gamma forecast"} {
		storeTestEntry(t, s, title, "content about "+title)
	}

	all, _ := s.ListEntries(ctx, storage.ListFilter{Limit: 10})
	second, _ := s.ListEntries(ctx, storage.ListFilter{Limit: 10, Offset: 1})
	if len(second) != len(all)-1 {
		t.Errorf("offset 1: want %d entries, got %d", len(all)-1, len(second))
	}

	results, err := s.ListEntries(ctx, storage.ListFilter{Limit: 10, Search: "Alpha"})
	if err != nil {
		t.Fatalf("ListEntries search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search 'Alpha': want 1 result, got %d", len(results))
	}
	if results[0].Title != "Alpha analytics" {
		t.Errorf("wrong entry: %q", results[0].Title)
	}
}

func TestApproveAndRejectEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Store a test entry using existing helpers
	entry := storage.KnowledgeEntry{
		Type:    storage.KTPrompt,
		Title:   "pending entry",
		Content: "content",
		Domain:  "test",
	}
	id, err := s.StoreEntry(ctx, entry, nil)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	// default status should be "approved" for migration compat
	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if e.Status != "approved" {
		t.Errorf("default Status = %q, want approved", e.Status)
	}

	if err := s.RejectEntry(ctx, id); err != nil {
		t.Fatalf("RejectEntry: %v", err)
	}
	e, _ = s.GetEntry(ctx, id)
	if e.Status != "rejected" {
		t.Errorf("Status after reject = %q", e.Status)
	}

	if err := s.ApproveEntry(ctx, id); err != nil {
		t.Fatalf("ApproveEntry: %v", err)
	}
	e, _ = s.GetEntry(ctx, id)
	if e.Status != "approved" {
		t.Errorf("Status after approve = %q", e.Status)
	}
}

func TestListEntries_StatusFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := storage.KnowledgeEntry{Type: storage.KTPrompt, Title: "pending", Content: "content", Domain: "test"}
	id, _ := s.StoreEntry(ctx, entry, nil)
	_ = s.RejectEntry(ctx, id)

	pending, _ := s.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "rejected"})
	approved, _ := s.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "approved"})

	if len(pending) != 1 {
		t.Errorf("rejected filter: want 1, got %d", len(pending))
	}
	if len(approved) != 0 {
		t.Errorf("approved filter: want 0, got %d", len(approved))
	}
}

func TestUpdateEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := storage.KnowledgeEntry{Type: storage.KTPrompt, Title: "original title", Content: "original content", Domain: "test"}
	id, _ := s.StoreEntry(ctx, entry, nil)

	e, _ := s.GetEntry(ctx, id)
	e.Title = "updated title"
	e.Content = "updated content"
	if err := s.UpdateEntry(ctx, *e); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	got, _ := s.GetEntry(ctx, id)
	if got.Title != "updated title" {
		t.Errorf("Title = %q", got.Title)
	}
}
