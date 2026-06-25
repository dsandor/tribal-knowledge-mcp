package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

func TestMoveKnowledge(t *testing.T) {
	t.Run("non-superadmin is forbidden", func(t *testing.T) {
		// Default role is "admin" — not superadmin — so the route group rejects it.
		store := &mockStore{apiKeyRole: "admin"}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/move", `{"entry_ids":["e1"],"team_id":"team-b"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin with valid body moves entries", func(t *testing.T) {
		store := &mockStore{
			apiKeyRole: "superadmin",
			teams:      map[string]storage.Team{"team-b": {ID: "team-b", Name: "Team B"}},
		}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/move", `{"entry_ids":["e1","e2"],"team_id":"team-b"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin with empty entry_ids is rejected", func(t *testing.T) {
		store := &mockStore{
			apiKeyRole: "superadmin",
			teams:      map[string]storage.Team{"team-b": {ID: "team-b", Name: "Team B"}},
		}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/move", `{"entry_ids":[],"team_id":"team-b"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin with unknown team is rejected", func(t *testing.T) {
		store := &mockStore{apiKeyRole: "superadmin"} // no teams registered
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/move", `{"entry_ids":["e1"],"team_id":"nope"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// newCopyServer builds a Server with non-nil AI sources so CopyEntryToTeam can
// re-embed (its src.Embedder call would panic on a nil *aiconfig.Sources).
// newReembedSources supplies a team-agnostic stub embedder.
func newCopyServer(t *testing.T, store *mockStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store).WithAISources(newReembedSources(store))
}

func TestCopyKnowledge(t *testing.T) {
	t.Run("non-superadmin is forbidden", func(t *testing.T) {
		store := &mockStore{apiKeyRole: "admin"}
		srv := newCopyServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/copy", `{"entry_ids":["e1"],"team_ids":["team-b"]}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin copies one entry into two teams", func(t *testing.T) {
		store := &mockStore{
			apiKeyRole: "superadmin",
			// GetEntry must succeed: register the source entry.
			entries: []storage.KnowledgeEntry{{
				ID:      "e1",
				Type:    storage.KTPrompt,
				Title:   "Earnings Summary Prompt",
				Content: "Summarize the earnings call focusing on guidance and margins.",
				Author:  "alice",
				TeamID:  "test-team",
			}},
			// Both targets must validate via GetTeam.
			teams: map[string]storage.Team{
				"team-b": {ID: "team-b", Name: "Team B"},
				"team-c": {ID: "team-c", Name: "Team C"},
			},
		}
		srv := newCopyServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/copy", `{"entry_ids":["e1"],"team_ids":["team-b","team-c"]}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			OK     bool `json:"ok"`
			Copied int  `json:"copied"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.OK || resp.Copied != 2 {
			t.Fatalf("want ok=true copied=2, got ok=%v copied=%d", resp.OK, resp.Copied)
		}
	})

	t.Run("superadmin with empty team_ids is rejected", func(t *testing.T) {
		store := &mockStore{apiKeyRole: "superadmin"}
		srv := newCopyServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/copy", `{"entry_ids":["e1"],"team_ids":[]}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("superadmin with unknown team is rejected", func(t *testing.T) {
		store := &mockStore{
			apiKeyRole: "superadmin",
			entries:    []storage.KnowledgeEntry{{ID: "e1", TeamID: "test-team"}},
		}
		srv := newCopyServer(t, store)

		req := authRequest("POST", "/api/admin/knowledge/copy", `{"entry_ids":["e1"],"team_ids":["nope"]}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}
