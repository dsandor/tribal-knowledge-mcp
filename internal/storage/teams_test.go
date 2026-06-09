package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// newTestStoreInternal opens a temp SQLite DB for internal package tests.
func newTestStoreInternal(t *testing.T) *SQLiteStore {
	t.Helper()
	f, err := os.CreateTemp("", "teams-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })

	store, err := NewSQLiteStore(path, 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetTeam(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	id, err := s.CreateTeam(ctx, Team{
		Name:           "acme",
		DomainPatterns: []string{`.*@acme\.com$`},
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	team, err := s.GetTeam(ctx, id)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if team.Name != "acme" {
		t.Errorf("Name = %q, want acme", team.Name)
	}
	if !team.Enabled {
		t.Error("Enabled should be true")
	}
	if len(team.DomainPatterns) != 1 || team.DomainPatterns[0] != `.*@acme\.com$` {
		t.Errorf("DomainPatterns = %v", team.DomainPatterns)
	}
}

func TestListTeams(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	for _, name := range []string{"alpha", "beta"} {
		if _, err := s.CreateTeam(ctx, Team{Name: name, Enabled: true}); err != nil {
			t.Fatalf("CreateTeam %s: %v", name, err)
		}
	}
	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) != 2 {
		t.Errorf("want 2 teams, got %d", len(teams))
	}
}

func TestSetTeamEnabled(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{Name: "t1", Enabled: true})
	if err := s.SetTeamEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetTeamEnabled: %v", err)
	}
	team, _ := s.GetTeam(ctx, id)
	if team.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestDeleteTeam(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{Name: "del", Enabled: true})
	if err := s.DeleteTeam(ctx, id); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	if _, err := s.GetTeam(ctx, id); err == nil {
		t.Error("expected error after delete")
	}
}

func TestUpsertAndGetUser(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	id1, err := s.UpsertUser(ctx, User{Email: "alice@acme.com", Name: "Alice", Role: "member"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	// upsert same email — should return same id
	id2, err := s.UpsertUser(ctx, User{Email: "alice@acme.com", Name: "Alice Updated", Role: "curator"})
	if err != nil {
		t.Fatalf("UpsertUser second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on upsert, got %q and %q", id1, id2)
	}
	u, err := s.GetUserByEmail(ctx, "alice@acme.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.Name != "Alice Updated" {
		t.Errorf("Name = %q", u.Name)
	}
}

func TestAssignUserToTeam(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})
	userID, _ := s.UpsertUser(ctx, User{Email: "bob@example.com", Role: "member"})
	if err := s.AssignUserToTeam(ctx, userID, teamID, "curator"); err != nil {
		t.Fatalf("AssignUserToTeam: %v", err)
	}
	u, _ := s.GetUserByEmail(ctx, "bob@example.com")
	if u.TeamID != teamID {
		t.Errorf("TeamID = %q, want %q", u.TeamID, teamID)
	}
	if u.Role != "curator" {
		t.Errorf("Role = %q, want curator", u.Role)
	}
}

func TestResolveTeamByEmail(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	id, _ := s.CreateTeam(ctx, Team{
		Name:           "acme",
		DomainPatterns: []string{`.*@acme\.com$`},
		Enabled:        true,
	})

	team, err := s.ResolveTeamByEmail(ctx, "user@acme.com")
	if err != nil {
		t.Fatalf("ResolveTeamByEmail: %v", err)
	}
	if team == nil || team.ID != id {
		t.Errorf("expected team %q, got %v", id, team)
	}

	none, err := s.ResolveTeamByEmail(ctx, "user@other.com")
	if err != nil {
		t.Fatalf("ResolveTeamByEmail no match: %v", err)
	}
	if none != nil {
		t.Errorf("expected nil for non-matching email")
	}
}

func TestCreateAndGetAPIKey(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})

	key := APIKey{
		ID:      "key-1",
		TeamID:  teamID,
		KeyType: APIKeyTypeTeam,
		Name:    "ci-key",
		KeyHash: "abc123hash",
		Role:    "admin",
	}
	if err := s.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	got, err := s.GetAPIKeyByHash(ctx, "abc123hash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got.TeamID != teamID {
		t.Errorf("TeamID = %q", got.TeamID)
	}
}

func TestRevokeAPIKey(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if err := s.CreateAPIKey(ctx, APIKey{ID: "k1", KeyHash: "h1", KeyType: APIKeyTypeTeam, Role: "member"}); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if err := s.RevokeAPIKey(ctx, "k1"); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := s.GetAPIKeyByHash(ctx, "h1"); err == nil {
		t.Error("expected error after revoke")
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	userID, _ := s.UpsertUser(ctx, User{Email: "c@d.com", Role: "member"})

	sess := Session{
		ID:        "s1",
		UserID:    userID,
		TokenHash: "th1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := s.GetSession(ctx, "th1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID = %q", got.UserID)
	}
	if err := s.DeleteSession(ctx, "th1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession(ctx, "th1"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestTeamSettings(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "t", Enabled: true})

	settings := TeamSettings{
		TeamID:             teamID,
		Domains:            []string{"finance", "legal"},
		ClusterThreshold:   0.9,
		PipelineMinEntries: 5,
		AgentModel:         "claude-haiku-4-5-20251001",
	}
	if err := s.PutTeamSettings(ctx, settings); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}
	got, err := s.GetTeamSettings(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeamSettings: %v", err)
	}
	if len(got.Domains) != 2 {
		t.Errorf("Domains = %v", got.Domains)
	}
	if got.ClusterThreshold != 0.9 {
		t.Errorf("ClusterThreshold = %v", got.ClusterThreshold)
	}
}

func TestAuthConfig(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	cfg := AuthConfig{
		Provider:        "oidc",
		OIDCIssuer:      "https://clerk.example.com",
		OIDCClientID:    "client-id-123",
		OIDCRedirectURL: "http://localhost:8080/auth/oidc/callback",
	}
	if err := s.PutAuthConfig(ctx, cfg); err != nil {
		t.Fatalf("PutAuthConfig: %v", err)
	}
	got, err := s.GetAuthConfig(ctx)
	if err != nil {
		t.Fatalf("GetAuthConfig: %v", err)
	}
	if got.OIDCIssuer != "https://clerk.example.com" {
		t.Errorf("OIDCIssuer = %q", got.OIDCIssuer)
	}
}

func TestLogAndQueryActivity(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.LogActivity(ctx, ActivityEntry{
			ID:         fmt.Sprintf("a%d", i),
			TeamID:     "team-1",
			Action:     "knowledge.store",
			EntityType: "entry",
			EntityID:   fmt.Sprintf("entry-%d", i),
		}); err != nil {
			t.Fatalf("LogActivity: %v", err)
		}
	}
	entries, err := s.QueryActivity(ctx, "team-1", 10)
	if err != nil {
		t.Fatalf("QueryActivity: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("want 3 entries, got %d", len(entries))
	}
}
