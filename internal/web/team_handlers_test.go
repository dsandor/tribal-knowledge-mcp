package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func TestMyTeams(t *testing.T) {
	// User-scoped key (apiKeyUserID set) routes through ListUserTeams.
	store := &mockStore{
		apiKeyUserID: "user-1",
		userTeams: []storage.Team{
			{ID: "team-a", Name: "Team A"},
			{ID: "team-b", Name: "Team B"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/me/teams", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
		ActiveTeam string `json:"active_team"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	ids := map[string]bool{}
	for _, tm := range resp.Teams {
		ids[tm.ID] = true
	}
	if !ids["team-a"] || !ids["team-b"] {
		t.Errorf("teams missing expected ids, got %+v", resp.Teams)
	}
	// active_team is set by the bearer key's TeamID (mockStore -> "test-team").
	if resp.ActiveTeam == "" {
		t.Errorf("active_team should be present, got empty")
	}
}

func TestListUserTeams(t *testing.T) {
	store := &mockStore{
		apiKeyRole: "admin",
		// Target user's home team matches the admin caller's team (test-team),
		// so the IDOR guard (authorizeTeamManage) permits the read.
		users: map[string]storage.User{
			"user-1": {ID: "user-1", TeamID: "test-team"},
		},
		userTeams: []storage.Team{
			{ID: "team-a", Name: "Team A"},
			{ID: "team-b", Name: "Team B"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/admin/users/user-1/teams", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := map[string]bool{}
	for _, tm := range resp.Teams {
		ids[tm.ID] = true
	}
	if !ids["team-a"] || !ids["team-b"] {
		t.Errorf("teams missing expected ids, got %+v", resp.Teams)
	}
}

func TestMembershipEndpoints(t *testing.T) {
	t.Run("superadmin adds membership to any team", func(t *testing.T) {
		store := &mockStore{apiKeyRole: "superadmin"}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/users/user-1/teams", `{"team_id":"team-x"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("want 2xx, got %d: %s", w.Code, w.Body.String())
		}
		if !store.addMemberCalled {
			t.Errorf("AddTeamMember was not invoked")
		}
	})

	t.Run("admin adds to a team they belong to", func(t *testing.T) {
		store := &mockStore{
			apiKeyUserID: "admin-1",
			memberOf:     map[string]bool{"team-x": true},
		}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/users/user-1/teams", `{"team_id":"team-x"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("want 2xx, got %d: %s", w.Code, w.Body.String())
		}
		if !store.addMemberCalled {
			t.Errorf("AddTeamMember was not invoked")
		}
	})

	t.Run("admin adds to a team they do NOT belong to", func(t *testing.T) {
		store := &mockStore{
			apiKeyUserID: "admin-1",
			memberOf:     map[string]bool{}, // not a member of team-x
		}
		srv := newTestServer(t, store)

		req := authRequest("POST", "/api/admin/users/user-1/teams", `{"team_id":"team-x"}`)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		if store.addMemberCalled {
			t.Errorf("AddTeamMember should not be invoked when forbidden")
		}
	})
}
