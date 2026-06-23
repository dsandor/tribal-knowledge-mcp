package auth

import "testing"

func TestListScopeTeamID(t *testing.T) {
	cases := []struct {
		name string
		tc   TeamContext
		want string
	}{
		{"member scoped to own team", TeamContext{Role: "member", TeamID: "t1"}, "t1"},
		{"admin scoped to own team", TeamContext{Role: "admin", TeamID: "t1"}, "t1"},
		{"superadmin sees all", TeamContext{Role: "superadmin", TeamID: "t1"}, ""},
		{"superadmin empty stays empty", TeamContext{Role: "superadmin"}, ""},
	}
	for _, c := range cases {
		if got := c.tc.ListScopeTeamID(); got != c.want {
			t.Errorf("%s: ListScopeTeamID() = %q, want %q", c.name, got, c.want)
		}
	}
}
