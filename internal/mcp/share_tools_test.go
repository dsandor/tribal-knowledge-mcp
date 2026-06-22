package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/auth"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	"github.com/mark3labs/mcp-go/server"
)

// shareStore implements the share methods with real single-use semantics,
// layered on mockStore for everything else.
type shareStore struct {
	mockStore
	shares  map[string]*storage.KnowledgeShare
	counter int
}

func newShareStore() *shareStore {
	return &shareStore{shares: map[string]*storage.KnowledgeShare{}}
}

func (s *shareStore) CreateShare(_ context.Context, entryID, sourceTeamID, createdBy string) (storage.KnowledgeShare, error) {
	s.counter++
	id := "share-" + string(rune('a'+s.counter))
	sh := &storage.KnowledgeShare{ID: id, EntryID: entryID, SourceTeamID: sourceTeamID, CreatedBy: createdBy}
	s.shares[id] = sh
	return *sh, nil
}

func (s *shareStore) GetShare(_ context.Context, id string) (*storage.KnowledgeShare, error) {
	sh, ok := s.shares[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	cp := *sh
	return &cp, nil
}

func (s *shareStore) MarkShareUsed(_ context.Context, id, usedBy, importedEntryID string) error {
	sh, ok := s.shares[id]
	if !ok {
		return storage.ErrNotFound
	}
	if sh.UsedAt != nil || sh.RevokedAt != nil {
		return storage.ErrNotFound // already consumed
	}
	now := time.Now()
	sh.UsedAt = &now
	sh.UsedBy = usedBy
	sh.ImportedEntryID = importedEntryID
	return nil
}

func TestKnowledgeShare_ReturnsShareIDAndURL(t *testing.T) {
	ctx := auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: "teamA", UserID: "alice", Role: "member",
	})
	store := newShareStore()
	store.entries = append(store.entries, storage.KnowledgeEntry{
		ID: "e1", Title: "T", Content: "C", Type: storage.KTPrompt, TeamID: "teamA", Author: "alice",
	})
	src := newTestSources(&mockEmbedder{embedding: []float32{0.1, 0.2}}, nil)

	handler := internalmcp.HandleKnowledgeShare(store, src)
	result, err := handler(ctx, callReq("entry_id", "e1"))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", textContent(result))
	}

	var res struct {
		ShareID string `json:"share_id"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal([]byte(textContent(result)), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", textContent(result), err)
	}
	if res.ShareID == "" {
		t.Errorf("empty share_id in %q", textContent(result))
	}
	if len(res.URL) < 7 || res.URL[:7] != "/share/" {
		t.Errorf("url = %q, want /share/<id>", res.URL)
	}
}

func TestKnowledgeImport_ImportsPending(t *testing.T) {
	store := newShareStore()
	store.entries = append(store.entries, storage.KnowledgeEntry{
		ID: "e1", Title: "T", Content: "C", Type: storage.KTPrompt, TeamID: "teamA", Author: "alice",
	})
	sh, _ := store.CreateShare(context.Background(), "e1", "teamA", "alice")
	src := newTestSources(&mockEmbedder{embedding: []float32{0.1, 0.2}}, nil)

	ctxB := auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: "teamB", UserID: "bob", Role: "member",
	})
	handler := internalmcp.HandleKnowledgeImport(store, src)

	result, err := handler(ctxB, callReq("share_id", sh.ID))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %q", textContent(result))
	}
	var res struct {
		ImportedEntryID string `json:"imported_entry_id"`
		Status          string `json:"status"`
	}
	if err := json.Unmarshal([]byte(textContent(result)), &res); err != nil {
		t.Fatalf("unmarshal %q: %v", textContent(result), err)
	}
	if res.ImportedEntryID == "" {
		t.Errorf("empty imported_entry_id in %q", textContent(result))
	}
	if res.Status != "pending" {
		t.Errorf("status = %q, want pending", res.Status)
	}

	// second import of same token must error.
	result2, err := handler(ctxB, callReq("share_id", sh.ID))
	if err != nil {
		t.Fatalf("handler err (2): %v", err)
	}
	if !result2.IsError {
		t.Errorf("second import did not error: %q", textContent(result2))
	}
}

// TestRegisterShareTools ensures registration wires up without panicking.
func TestRegisterShareTools(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0")
	internalmcp.RegisterShareTools(s, newShareStore(), newTestSources(&mockEmbedder{embedding: []float32{0.1}}, nil))
}
