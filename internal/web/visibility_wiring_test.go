package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// TestHandleKnowledgeList_UserVisibilityFilter verifies handleKnowledgeList
// applies the per-user suppression filter: a user-scoped request whose user has
// an item rule hiding entry X excludes X, while a team-scoped request (empty
// UserID) includes it.
func TestHandleKnowledgeList_UserVisibilityFilter(t *testing.T) {
	entries := []storage.KnowledgeEntry{
		{ID: "X", Title: "X", Author: "carol", TeamID: "test-team"},
		{ID: "Y", Title: "Y", Author: "carol", TeamID: "test-team"},
	}

	// User-scoped request: user-a hides X.
	userStore := &mockStore{
		entries:      entries,
		apiKeyUserID: "user-a",
		visRules: map[string][]storage.VisibilityRule{
			"user-a": {{RuleType: "item", Value: "X"}},
		},
		users: map[string]storage.User{
			"user-a": {ID: "user-a", Email: "a@example.com", Name: "Alice"},
		},
	}
	srv := newTestServer(t, userStore)
	req := authRequest("GET", "/api/knowledge?limit=10", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	ids := decodeIDs(t, w)
	if ids["X"] {
		t.Errorf("user-a should NOT see X")
	}
	if !ids["Y"] {
		t.Errorf("user-a should see Y")
	}

	// Team-scoped request (no UserID): no filtering.
	teamStore := &mockStore{entries: entries} // apiKeyUserID empty
	srv = newTestServer(t, teamStore)
	req = authRequest("GET", "/api/knowledge?limit=10", "")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	ids = decodeIDs(t, w)
	if !ids["X"] || !ids["Y"] {
		t.Errorf("team-scoped request should see both X and Y, got %v", ids)
	}
}

func decodeIDs(t *testing.T, w *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	var entries []storage.KnowledgeEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	return ids
}
