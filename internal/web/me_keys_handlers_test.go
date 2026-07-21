package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

// decodeSafeKeys decodes the /api/me/api-keys response body into the fields
// the test cares about (id is enough to assert set membership).
func decodeSafeKeys(t *testing.T, w *httptest.ResponseRecorder) []struct {
	ID string `json:"id"`
} {
	t.Helper()
	var out []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return out
}

func ids(rows []struct {
	ID string `json:"id"`
}) map[string]bool {
	m := map[string]bool{}
	for _, r := range rows {
		m[r.ID] = true
	}
	return m
}

// A member caller must see only their own user-scoped keys — never another
// user's personal key, and never team-scoped keys (those are admin-or-above).
func TestHandleMyAPIKeys_MemberSeesOnlyOwnUserKeys(t *testing.T) {
	store := &mockStore{
		apiKeyUserID: "user-1",
		apiKeyRole:   "member",
		apiKeysList: []storage.APIKey{
			{ID: "mine", TeamID: "test-team", UserID: "user-1", KeyType: storage.APIKeyTypeUser, Name: "mine"},
			{ID: "other-user", TeamID: "test-team", UserID: "user-2", KeyType: storage.APIKeyTypeUser, Name: "not-mine"},
			{ID: "team-key", TeamID: "test-team", KeyType: storage.APIKeyTypeTeam, Name: "team-key"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/me/api-keys", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := ids(decodeSafeKeys(t, w))
	if len(got) != 1 || !got["mine"] {
		t.Errorf("want only {mine}, got %+v", got)
	}
}

// An admin caller sees their own user keys plus team keys, but still never
// another user's personal key.
func TestHandleMyAPIKeys_AdminSeesOwnKeysPlusTeamKeys(t *testing.T) {
	store := &mockStore{
		apiKeyUserID: "admin-1",
		apiKeyRole:   "admin",
		apiKeysList: []storage.APIKey{
			{ID: "mine", TeamID: "test-team", UserID: "admin-1", KeyType: storage.APIKeyTypeUser, Name: "mine"},
			{ID: "other-user", TeamID: "test-team", UserID: "user-2", KeyType: storage.APIKeyTypeUser, Name: "not-mine"},
			{ID: "team-key", TeamID: "test-team", KeyType: storage.APIKeyTypeTeam, Name: "team-key"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/me/api-keys", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := ids(decodeSafeKeys(t, w))
	if len(got) != 2 || !got["mine"] || !got["team-key"] {
		t.Errorf("want {mine, team-key}, got %+v", got)
	}
}

// A caller whose TeamContext has an empty UserID (e.g. a team-scoped bearer
// key) must never match a stored user-key that also has an empty UserID —
// the handler must require UserID != "" on both sides, not just equality.
func TestHandleMyAPIKeys_EmptyContextUserIDMatchesNoUserKeys(t *testing.T) {
	store := &mockStore{
		apiKeyUserID: "", // team-scoped key => tc.UserID == ""
		apiKeyRole:   "member",
		apiKeysList: []storage.APIKey{
			{ID: "orphan-user-key", TeamID: "test-team", UserID: "", KeyType: storage.APIKeyTypeUser, Name: "orphan"},
			{ID: "team-key", TeamID: "test-team", KeyType: storage.APIKeyTypeTeam, Name: "team-key"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/me/api-keys", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeSafeKeys(t, w)
	if len(got) != 0 {
		t.Errorf("want no keys, got %+v", got)
	}
}
