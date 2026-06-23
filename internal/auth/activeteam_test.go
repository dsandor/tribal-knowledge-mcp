package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeMembers struct{ member map[string]bool }

func (f fakeMembers) IsTeamMember(_ context.Context, userID, teamID string) (bool, error) {
	return f.member[userID+"|"+teamID], nil
}

func TestActiveTeamMiddleware(t *testing.T) {
	store := fakeMembers{member: map[string]bool{"u1|tB": true}}
	mw := ActiveTeamMiddleware(store)

	run := func(tc TeamContext, header string) (int, string) {
		var gotTeam string
		h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotTeam = GetTeamContext(r.Context()).TeamID
		}))
		req := httptest.NewRequest("GET", "/", nil)
		if header != "" {
			req.Header.Set("X-Team-Id", header)
		}
		req = req.WithContext(WithTestTeamContext(req.Context(), tc))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, gotTeam
	}

	// member overriding to a team they belong to
	if code, team := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, "tB"); code != 200 || team != "tB" {
		t.Errorf("member->tB: code=%d team=%q", code, team)
	}
	// member overriding to a team they do NOT belong to => 403
	if code, _ := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, "tC"); code != 403 {
		t.Errorf("member->tC: want 403, got %d", code)
	}
	// superadmin can override anywhere
	if code, team := run(TeamContext{UserID: "s1", Role: "superadmin"}, "tZ"); code != 200 || team != "tZ" {
		t.Errorf("superadmin->tZ: code=%d team=%q", code, team)
	}
	// plain team key (no user) cannot switch to a foreign team => 403
	if code, _ := run(TeamContext{KeyID: "k1", KeyType: "team", TeamID: "tA", Role: "member"}, "tB"); code != 403 {
		t.Errorf("teamkey->tB: want 403, got %d", code)
	}
	// no header => unchanged
	if code, team := run(TeamContext{UserID: "u1", TeamID: "tA", Role: "member"}, ""); code != 200 || team != "tA" {
		t.Errorf("no header: code=%d team=%q", code, team)
	}
}
