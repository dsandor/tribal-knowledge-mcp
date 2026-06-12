package storage

import (
	"context"
	"errors"
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

func TestTeamSettingsAITouchpointsRoundTrip(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "touchpoints-test", Enabled: true})

	// Round-trip with a non-empty touchpoints map.
	settings := TeamSettings{
		TeamID:             teamID,
		Domains:            []string{},
		ClusterThreshold:   0.85,
		PipelineMinEntries: 10,
		AgentModel:         "claude-haiku-4-5-20251001",
		AITouchpoints: map[string]AITouchpoint{
			"analysis": {Provider: "ollama", Model: "llama3.1"},
		},
	}
	if err := s.PutTeamSettings(ctx, settings); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}
	got, err := s.GetTeamSettings(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeamSettings: %v", err)
	}
	if got.AITouchpoints == nil {
		t.Fatal("AITouchpoints should not be nil")
	}
	tp, ok := got.AITouchpoints["analysis"]
	if !ok {
		t.Fatalf("AITouchpoints[analysis] not found; got: %v", got.AITouchpoints)
	}
	if tp.Provider != "ollama" {
		t.Errorf("Provider = %q, want ollama", tp.Provider)
	}
	if tp.Model != "llama3.1" {
		t.Errorf("Model = %q, want llama3.1", tp.Model)
	}

	// Put with nil map → Get returns non-nil empty map.
	settings2 := TeamSettings{
		TeamID:             teamID,
		Domains:            []string{},
		ClusterThreshold:   0.85,
		PipelineMinEntries: 10,
		AgentModel:         "claude-haiku-4-5-20251001",
		AITouchpoints:      nil,
	}
	if err := s.PutTeamSettings(ctx, settings2); err != nil {
		t.Fatalf("PutTeamSettings (nil map): %v", err)
	}
	got2, err := s.GetTeamSettings(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeamSettings (nil map): %v", err)
	}
	if got2.AITouchpoints == nil {
		t.Error("AITouchpoints should be non-nil (empty map) after nil Put")
	}
	if len(got2.AITouchpoints) != 0 {
		t.Errorf("AITouchpoints should be empty map, got: %v", got2.AITouchpoints)
	}
}

func TestTeamSettingsLLMProviderRoundTrip(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	teamID, _ := s.CreateTeam(ctx, Team{Name: "llm-test", Enabled: true})

	settings := TeamSettings{
		TeamID:             teamID,
		Domains:            []string{},
		ClusterThreshold:   0.85,
		PipelineMinEntries: 10,
		AgentModel:         "claude-haiku-4-5-20251001",
		LLMProvider:        "ollama",
		OllamaLLMModel:     "llama3.1",
	}
	if err := s.PutTeamSettings(ctx, settings); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}
	got, err := s.GetTeamSettings(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeamSettings: %v", err)
	}
	if got.LLMProvider != "ollama" {
		t.Errorf("LLMProvider = %q, want ollama", got.LLMProvider)
	}
	if got.OllamaLLMModel != "llama3.1" {
		t.Errorf("OllamaLLMModel = %q, want llama3.1", got.OllamaLLMModel)
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

// ── Team deletion tests ───────────────────────────────────────────────────────

// seedTeamData seeds one of each seeable category into the given team and
// returns the agent ID (for later verification).
func seedTeamData(t *testing.T, s *SQLiteStore, ctx context.Context, teamID string, domain string) (agentID string) {
	t.Helper()

	// 1. User (UpsertUser + AssignUserToTeam)
	email := domain + "-user@test.example"
	uid, err := s.UpsertUser(ctx, User{Email: email, Name: "Test", Role: "member"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := s.AssignUserToTeam(ctx, uid, teamID, "member"); err != nil {
		t.Fatalf("AssignUserToTeam: %v", err)
	}

	// 2. API key (CreateAPIKey)
	if err := s.CreateAPIKey(ctx, APIKey{
		ID:      domain + "-key",
		TeamID:  teamID,
		KeyType: APIKeyTypeTeam,
		Name:    "ci",
		KeyHash: domain + "-hash",
		Role:    "member",
	}); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// 3. Entry (StoreEntry)
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{
		Type:   "prompt",
		Title:  domain + " entry",
		TeamID: teamID,
	}, nil); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	// 4. Cluster (StoreCluster)
	if _, err := s.StoreCluster(ctx, Cluster{
		Domain: domain,
		Title:  domain + " cluster",
		TeamID: teamID,
	}); err != nil {
		t.Fatalf("StoreCluster: %v", err)
	}

	// 5. Agent (UpsertAgent) + AgentVersion (StoreAgentVersion)
	agentID, err = s.UpsertAgent(ctx, Agent{
		Domain:       domain,
		TeamID:       teamID,
		Status:       AgentStatusDraft,
		SystemPrompt: "sys-" + domain,
	})
	if err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if err := s.StoreAgentVersion(ctx, AgentVersion{
		AgentID:      agentID,
		Version:      1,
		SystemPrompt: "v1",
	}); err != nil {
		t.Fatalf("StoreAgentVersion: %v", err)
	}

	// 6. Rule (StoreRule — inserts with team_id=''; we patch it via raw SQL since
	//    StoreRule's INSERT doesn't accept a team_id parameter yet)
	ruleID, err := s.StoreRule(ctx, Rule{
		Title:   domain + " rule",
		Content: "rule content",
		Scope:   RuleScopeTeam,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("StoreRule: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE rules SET team_id = ? WHERE id = ?", teamID, ruleID,
	); err != nil {
		t.Fatalf("patch rule team_id: %v", err)
	}

	return agentID
}

func TestTeamDataCounts(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	t1, err := s.CreateTeam(ctx, Team{Name: "t1", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam t1: %v", err)
	}
	t2, err := s.CreateTeam(ctx, Team{Name: "t2", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam t2: %v", err)
	}

	// Seed t1 with one of each category.
	_ = seedTeamData(t, s, ctx, t1, "dom1")

	// t1 should have non-zero counts.
	counts, err := s.TeamDataCounts(ctx, t1)
	if err != nil {
		t.Fatalf("TeamDataCounts t1: %v", err)
	}
	if counts.Users != 1 {
		t.Errorf("Users = %d, want 1", counts.Users)
	}
	if counts.APIKeys != 1 {
		t.Errorf("APIKeys = %d, want 1", counts.APIKeys)
	}
	if counts.Entries != 1 {
		t.Errorf("Entries = %d, want 1", counts.Entries)
	}
	if counts.Clusters != 1 {
		t.Errorf("Clusters = %d, want 1", counts.Clusters)
	}
	if counts.Agents != 1 {
		t.Errorf("Agents = %d, want 1", counts.Agents)
	}
	if counts.Rules != 1 {
		t.Errorf("Rules = %d, want 1", counts.Rules)
	}

	// t2 is empty — all zeros.
	empty, err := s.TeamDataCounts(ctx, t2)
	if err != nil {
		t.Fatalf("TeamDataCounts t2: %v", err)
	}
	if empty != (TeamDataCounts{}) {
		t.Errorf("empty team counts = %+v, want all zeros", empty)
	}
}

func TestDeleteTeamMigrate(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	t1, err := s.CreateTeam(ctx, Team{Name: "source", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam source: %v", err)
	}
	t2, err := s.CreateTeam(ctx, Team{Name: "target", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam target: %v", err)
	}

	// Seed source with domain "d1".
	sourceAgentID := seedTeamData(t, s, ctx, t1, "d1")

	// Seed target with a conflicting agent of the SAME domain "d1".
	targetAgentID, err := s.UpsertAgent(ctx, Agent{
		Domain:       "d1",
		TeamID:       t2,
		Status:       AgentStatusDraft,
		SystemPrompt: "target-sys-d1",
	})
	if err != nil {
		t.Fatalf("UpsertAgent target: %v", err)
	}

	// Also create a team_settings row for the source.
	if err := s.PutTeamSettings(ctx, TeamSettings{
		TeamID:             t1,
		ClusterThreshold:   0.85,
		PipelineMinEntries: 10,
	}); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}

	summary, err := s.DeleteTeamMigrate(ctx, t1, t2)
	if err != nil {
		t.Fatalf("DeleteTeamMigrate: %v", err)
	}

	// The source's conflicting agent was skipped (deleted, not moved).
	if summary.AgentsSkipped != 1 {
		t.Errorf("AgentsSkipped = %d, want 1", summary.AgentsSkipped)
	}
	// The source agent was not moved (it was deleted because of conflict).
	if summary.Agents != 0 {
		t.Errorf("Agents (moved) = %d, want 0", summary.Agents)
	}
	// Users, entries, clusters, rules each had 1 row.
	if summary.Users != 1 {
		t.Errorf("Users = %d, want 1", summary.Users)
	}
	if summary.Entries != 1 {
		t.Errorf("Entries = %d, want 1", summary.Entries)
	}
	if summary.Clusters != 1 {
		t.Errorf("Clusters = %d, want 1", summary.Clusters)
	}
	if summary.Rules != 1 {
		t.Errorf("Rules = %d, want 1", summary.Rules)
	}
	// API key should be 1.
	if summary.APIKeys != 1 {
		t.Errorf("APIKeys = %d, want 1", summary.APIKeys)
	}

	// Source team is gone.
	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	for _, team := range teams {
		if team.ID == t1 {
			t.Errorf("source team still present after migration")
		}
	}

	// Source team_settings row is gone.
	row := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM team_settings WHERE team_id = ?", t1)
	var cnt int
	if err := row.Scan(&cnt); err != nil {
		t.Fatalf("count team_settings: %v", err)
	}
	if cnt != 0 {
		t.Errorf("source team_settings row still present after migration")
	}

	// The source's conflicting agent AND its versions are deleted.
	a, err := s.GetAgent(ctx, sourceAgentID)
	if err != nil {
		t.Fatalf("GetAgent source: %v", err)
	}
	if a != nil {
		t.Errorf("source conflicting agent should be deleted, still exists")
	}
	var verCnt int
	vRow := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_versions WHERE agent_id = ?", sourceAgentID)
	if err := vRow.Scan(&verCnt); err != nil {
		t.Fatalf("count agent_versions: %v", err)
	}
	if verCnt != 0 {
		t.Errorf("source agent versions should be deleted, count=%d", verCnt)
	}

	// Target's own agent for "d1" is untouched.
	ta, err := s.GetAgent(ctx, targetAgentID)
	if err != nil {
		t.Fatalf("GetAgent target: %v", err)
	}
	if ta == nil {
		t.Fatalf("target's own agent for d1 should still exist")
	}
	if ta.SystemPrompt != "target-sys-d1" {
		t.Errorf("target agent system_prompt = %q, want target-sys-d1", ta.SystemPrompt)
	}

	// Spot-check: entry now belongs to t2.
	entries, err := s.ListEntries(ctx, ListFilter{TeamID: t2})
	if err != nil {
		t.Fatalf("ListEntries t2: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("expected migrated entry in t2")
	}

	// Spot-check: cluster now belongs to t2.
	clusters, err := s.ListClusters(ctx, t2)
	if err != nil {
		t.Fatalf("ListClusters t2: %v", err)
	}
	if len(clusters) == 0 {
		t.Errorf("expected migrated cluster in t2")
	}

	// Spot-check: user now belongs to t2.
	users, err := s.ListUsers(ctx, t2)
	if err != nil {
		t.Fatalf("ListUsers t2: %v", err)
	}
	if len(users) == 0 {
		t.Errorf("expected migrated user in t2")
	}
}

func TestDeleteTeamMigrateValidation(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	t1, err := s.CreateTeam(ctx, Team{Name: "team1", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	t2, err := s.CreateTeam(ctx, Team{Name: "team2", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Same source == target → error.
	if _, err := s.DeleteTeamMigrate(ctx, t1, t1); err == nil {
		t.Error("expected error when source == target")
	}

	// Unknown target → error (not ErrNotFound, just a validation error).
	if _, err := s.DeleteTeamMigrate(ctx, t1, "nonexistent-team-id"); err == nil {
		t.Error("expected error with unknown target")
	}

	// Unknown source → ErrNotFound.
	_, err = s.DeleteTeamMigrate(ctx, "nonexistent-source-id", t2)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for unknown source, got %v", err)
	}
}

func TestDeleteTeamCleansSettings(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	id, err := s.CreateTeam(ctx, Team{Name: "cleanup", Enabled: true})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Create a settings row.
	if err := s.PutTeamSettings(ctx, TeamSettings{
		TeamID:             id,
		ClusterThreshold:   0.9,
		PipelineMinEntries: 5,
	}); err != nil {
		t.Fatalf("PutTeamSettings: %v", err)
	}

	// Verify settings row exists.
	row := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM team_settings WHERE team_id = ?", id)
	var cnt int
	if err := row.Scan(&cnt); err != nil {
		t.Fatalf("count settings before delete: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 settings row before delete, got %d", cnt)
	}

	// Delete the team.
	if err := s.DeleteTeam(ctx, id); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	// Team should be gone.
	if _, err := s.GetTeam(ctx, id); err == nil {
		t.Error("expected error after delete")
	}

	// Settings row should also be gone.
	row2 := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM team_settings WHERE team_id = ?", id)
	var cnt2 int
	if err := row2.Scan(&cnt2); err != nil {
		t.Fatalf("count settings after delete: %v", err)
	}
	if cnt2 != 0 {
		t.Errorf("expected 0 settings rows after delete, got %d", cnt2)
	}
}
