package storage

import (
	"context"
	"errors"
	"testing"
)

func TestAutoTagsRoundTrip(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	id, err := s.StoreEntry(ctx, KnowledgeEntry{
		Type: KTPattern, Title: "t", Content: "c",
		Tags: []string{"user-tag"},
	}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(e.AutoTags) != 0 {
		t.Fatalf("expected no auto tags, got %v", e.AutoTags)
	}

	if err := s.UpdateAutoTags(ctx, id, []string{"valuation", "banking"}); err != nil {
		t.Fatalf("update auto tags: %v", err)
	}
	e, err = s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if len(e.AutoTags) != 2 || e.AutoTags[0] != "valuation" || e.AutoTags[1] != "banking" {
		t.Fatalf("auto tags = %v, want [valuation banking]", e.AutoTags)
	}
	if len(e.Tags) != 1 || e.Tags[0] != "user-tag" {
		t.Fatalf("tags = %v, want [user-tag]", e.Tags)
	}
}

func TestUpdateAutoTagsNotFound(t *testing.T) {
	s := newTestStoreInternal(t)
	err := s.UpdateAutoTags(context.Background(), "no-such-id", []string{"x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListEntriesTagFilter(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	idUser, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "a", Content: "c", Tags: []string{"earnings"}}, nil)
	if err != nil {
		t.Fatalf("store user-tagged: %v", err)
	}
	idAuto, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "b", Content: "c"}, nil)
	if err != nil {
		t.Fatalf("store auto-tagged: %v", err)
	}
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "d", Content: "c", Tags: []string{"other"}}, nil); err != nil {
		t.Fatalf("store other-tagged: %v", err)
	}
	if err := s.UpdateAutoTags(ctx, idAuto, []string{"earnings"}); err != nil {
		t.Fatalf("update auto tags: %v", err)
	}

	got, err := s.ListEntries(ctx, ListFilter{Tag: "earnings"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	gotIDs := make(map[string]bool, len(got))
	for _, e := range got {
		gotIDs[e.ID] = true
	}
	if len(got) != 2 || !gotIDs[idUser] || !gotIDs[idAuto] {
		t.Fatalf("got entries %v, want exactly [%s %s]", gotIDs, idUser, idAuto)
	}

	got, err = s.ListEntries(ctx, ListFilter{Tag: "nope"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d entries for unknown tag, want 0", len(got))
	}
}
