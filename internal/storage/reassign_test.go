package storage

import (
	"context"
	"testing"
)

func TestReassignEntriesTeam(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	emb := []float32{0.1, 0.2, 0.3, 0.4}

	id1, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPrompt, Title: "e1", Content: "c1", TeamID: "A"}, emb)
	if err != nil {
		t.Fatalf("StoreEntry id1: %v", err)
	}
	id2, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPrompt, Title: "e2", Content: "c2", TeamID: "A"}, emb)
	if err != nil {
		t.Fatalf("StoreEntry id2: %v", err)
	}

	if err := s.ReassignEntriesTeam(ctx, []string{id1, id2}, "B"); err != nil {
		t.Fatalf("ReassignEntriesTeam: %v", err)
	}

	for _, id := range []string{id1, id2} {
		got, err := s.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("GetEntry(%s): %v", id, err)
		}
		if got.TeamID != "B" {
			t.Errorf("entry %s TeamID = %q, want %q", id, got.TeamID, "B")
		}
	}
}
