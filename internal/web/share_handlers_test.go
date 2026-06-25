package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// shareStore embeds mockStore and gives the share token methods real in-memory
// behaviour so the create/get/import HTTP flow can be exercised end to end. It
// also records the most recently stored (imported) entry.
type shareStore struct {
	mockStore
	shares    map[string]*storage.KnowledgeShare
	storedNew *storage.KnowledgeEntry // last entry persisted via StoreEntryChunked
	nextID    int
}

func newShareStore(userID string, entries ...storage.KnowledgeEntry) *shareStore {
	return &shareStore{
		mockStore: mockStore{apiKeyUserID: userID, entries: entries},
		shares:    map[string]*storage.KnowledgeShare{},
	}
}

func (s *shareStore) CreateShare(_ context.Context, entryID, sourceTeamID, createdBy string) (storage.KnowledgeShare, error) {
	s.nextID++
	sh := storage.KnowledgeShare{
		ID:           "share-token-1",
		EntryID:      entryID,
		SourceTeamID: sourceTeamID,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now(),
	}
	s.shares[sh.ID] = &sh
	return sh, nil
}

func (s *shareStore) GetShare(_ context.Context, id string) (*storage.KnowledgeShare, error) {
	if sh, ok := s.shares[id]; ok {
		cp := *sh
		return &cp, nil
	}
	return nil, storage.ErrNotFound
}

func (s *shareStore) MarkShareUsed(_ context.Context, id, usedBy, importedEntryID string) error {
	sh, ok := s.shares[id]
	if !ok {
		return storage.ErrNotFound
	}
	if sh.UsedAt != nil || sh.RevokedAt != nil {
		return storage.ErrNotFound // already burned
	}
	now := time.Now()
	sh.UsedAt = &now
	sh.UsedBy = usedBy
	sh.ImportedEntryID = importedEntryID
	return nil
}

func (s *shareStore) StoreEntryChunked(_ context.Context, e storage.KnowledgeEntry, _ []storage.EntryChunk) (string, error) {
	e.ID = "imported-1"
	s.storedNew = &e
	return e.ID, nil
}

var _ web.AllStore = (*shareStore)(nil)

func newShareServer(t *testing.T, store web.AllStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	// newReembedSources supplies a non-nil stub embedder for any team so import
	// can chunk + embed without real AI configuration.
	return web.NewServer(staticFS, store).WithAISources(newReembedSources(store))
}

// sourceEntry is a fixture entry owned by the caller's team ("test-team").
func sourceEntry() storage.KnowledgeEntry {
	return storage.KnowledgeEntry{
		ID:      "e1",
		Type:    storage.KTPrompt,
		Title:   "Earnings Summary Prompt",
		Content: "Summarize the earnings call focusing on guidance and margins.",
		Domain:  "finance",
		Author:  "alice",
		Tags:    []string{"earnings"},
		TeamID:  "test-team",
	}
}

// TestCreateShare_ReturnsIDAndURL verifies POST /api/knowledge/{id}/share mints a
// token and returns its id + relative URL.
func TestCreateShare_ReturnsIDAndURL(t *testing.T) {
	store := newShareStore("alice", sourceEntry())
	srv := newShareServer(t, store)

	req := authRequest("POST", "/api/knowledge/e1/share", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["share_id"] != "share-token-1" {
		t.Fatalf("share_id = %v, want share-token-1", resp["share_id"])
	}
	if resp["url"] != "/share/share-token-1" {
		t.Fatalf("url = %v, want /share/share-token-1", resp["url"])
	}
}

// TestGetShare_ReturnsPreviewImportable verifies GET /api/share/{token} returns
// the recipient-facing preview with importable=true for a fresh token. The
// source team here differs from the caller's team, so already_yours is false and
// no source-team access check blocks the view.
func TestGetShare_ReturnsPreviewImportable(t *testing.T) {
	entry := sourceEntry()
	entry.TeamID = "other-team" // owned by a different team than the caller
	store := newShareStore("bob", entry)
	// Pre-create the share as if minted by the source team.
	store.shares["share-token-1"] = &storage.KnowledgeShare{
		ID: "share-token-1", EntryID: "e1", SourceTeamID: "other-team", CreatedBy: "alice", CreatedAt: time.Now(),
	}
	srv := newShareServer(t, store)

	req := authRequest("GET", "/api/share/share-token-1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["title"] != "Earnings Summary Prompt" {
		t.Errorf("title = %v", resp["title"])
	}
	if resp["source_team_id"] != "other-team" {
		t.Errorf("source_team_id = %v, want other-team", resp["source_team_id"])
	}
	if resp["importable"] != true {
		t.Errorf("importable = %v, want true", resp["importable"])
	}
	if resp["already_yours"] != false {
		t.Errorf("already_yours = %v, want false", resp["already_yours"])
	}
	// Must not leak embeddings / internal fields.
	if _, leaked := resp["Embedding"]; leaked {
		t.Errorf("preview leaked embedding data")
	}
}

// TestGetShare_NotFound returns 404 for an unknown token.
func TestGetShare_NotFound(t *testing.T) {
	store := newShareStore("bob")
	srv := newShareServer(t, store)

	req := authRequest("GET", "/api/share/nope", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestImportShare_DifferentTeam_ReturnsPending verifies importing a share from a
// different source team copies the entry into the caller's team as pending and
// burns the token.
func TestImportShare_DifferentTeam_ReturnsPending(t *testing.T) {
	entry := sourceEntry()
	entry.TeamID = "other-team"
	store := newShareStore("bob", entry)
	store.shares["share-token-1"] = &storage.KnowledgeShare{
		ID: "share-token-1", EntryID: "e1", SourceTeamID: "other-team", CreatedBy: "alice", CreatedAt: time.Now(),
	}
	srv := newShareServer(t, store)

	req := authRequest("POST", "/api/share/share-token-1/import", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "pending" {
		t.Fatalf("status = %v, want pending", resp["status"])
	}
	if resp["imported_entry_id"] != "imported-1" {
		t.Fatalf("imported_entry_id = %v, want imported-1", resp["imported_entry_id"])
	}
	// Imported into the caller's team, marked pending.
	if store.storedNew == nil {
		t.Fatalf("no entry was stored")
	}
	if store.storedNew.TeamID != "test-team" {
		t.Errorf("imported TeamID = %q, want test-team", store.storedNew.TeamID)
	}
	if store.storedNew.Status != "pending" {
		t.Errorf("imported Status = %q, want pending", store.storedNew.Status)
	}
}

// TestImportShare_UsedToken_Returns409 verifies importing an already-used token
// yields a 409 conflict.
func TestImportShare_UsedToken_Returns409(t *testing.T) {
	entry := sourceEntry()
	entry.TeamID = "other-team"
	store := newShareStore("bob", entry)
	used := time.Now()
	store.shares["share-token-1"] = &storage.KnowledgeShare{
		ID: "share-token-1", EntryID: "e1", SourceTeamID: "other-team",
		CreatedBy: "alice", UsedAt: &used, UsedBy: "carol", CreatedAt: time.Now(),
	}
	srv := newShareServer(t, store)

	req := authRequest("POST", "/api/share/share-token-1/import", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestImportShare_SameTeam_ReturnsAlreadyYours verifies that importing a share
// whose source team equals the caller's team is a friendly no-op.
func TestImportShare_SameTeam_ReturnsAlreadyYours(t *testing.T) {
	store := newShareStore("bob", sourceEntry()) // entry owned by test-team
	store.shares["share-token-1"] = &storage.KnowledgeShare{
		ID: "share-token-1", EntryID: "e1", SourceTeamID: "test-team", CreatedBy: "alice", CreatedAt: time.Now(),
	}
	srv := newShareServer(t, store)

	req := authRequest("POST", "/api/share/share-token-1/import", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "already_yours" {
		t.Fatalf("status = %v, want already_yours", resp["status"])
	}
}
