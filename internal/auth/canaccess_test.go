package auth

import "testing"

func TestCanAccess(t *testing.T) {
	cases := []struct {
		name string
		tc   TeamContext
		team string
		want bool
	}{
		{"same team", TeamContext{TeamID: "t1", Role: "member"}, "t1", true},
		{"different team", TeamContext{TeamID: "t1", Role: "member"}, "t2", false},
		{"superadmin cross-team", TeamContext{TeamID: "t1", Role: "superadmin"}, "t2", true},
		{"empty caller team (dev bypass, stdio MCP)", TeamContext{Role: "superadmin"}, "t2", true},
		{"empty caller team plain", TeamContext{}, "t2", true},
		{"legacy resource without team", TeamContext{TeamID: "t1", Role: "member"}, "", true},
		{"curator different team", TeamContext{TeamID: "t1", Role: "curator"}, "t2", false},
		{"admin different team", TeamContext{TeamID: "t1", Role: "admin"}, "t2", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanAccess(c.tc, c.team); got != c.want {
				t.Errorf("CanAccess(%+v, %q) = %v, want %v", c.tc, c.team, got, c.want)
			}
		})
	}
}
