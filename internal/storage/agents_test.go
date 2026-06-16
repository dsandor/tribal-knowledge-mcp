package storage

import (
	"context"
	"testing"
)

func newTestAgentStore(t *testing.T) *SQLiteStore {
	t.Helper()
	return newTestAnalysisStore(t)
}

func TestUpsertAndGetAgent_Create(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	agent := Agent{
		Domain:       "finance",
		Version:      1,
		Status:       AgentStatusDraft,
		SystemPrompt: "You are a finance assistant.",
		Instructions: "Use DCF for valuation.",
		AntiPatterns: "Do not guess earnings.",
		SourceRefs:   []string{"cluster-1"},
		ClusterID:    "cluster-1",
	}

	id, err := s.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	got, err := s.GetAgent(ctx, id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Domain != "finance" {
		t.Errorf("domain = %q, want finance", got.Domain)
	}
	if got.SystemPrompt != "You are a finance assistant." {
		t.Errorf("system_prompt = %q", got.SystemPrompt)
	}
	if got.Status != AgentStatusDraft {
		t.Errorf("status = %q, want draft", got.Status)
	}
	if len(got.SourceRefs) != 1 || got.SourceRefs[0] != "cluster-1" {
		t.Errorf("source_refs = %v, want [cluster-1]", got.SourceRefs)
	}
}

func TestUpsertAgent_UpdateExisting(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, err := s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusDraft, SystemPrompt: "v1"})
	if err != nil {
		t.Fatalf("setup UpsertAgent: %v", err)
	}

	a2 := Agent{ID: id, Domain: "finance", Version: 2, Status: AgentStatusDraft, SystemPrompt: "v2"}
	id2, err := s.UpsertAgent(ctx, a2)
	if err != nil {
		t.Fatalf("UpsertAgent update: %v", err)
	}
	if id != id2 {
		t.Errorf("id changed on update: got %q, want %q", id2, id)
	}

	got, _ := s.GetAgent(ctx, id)
	if got.SystemPrompt != "v2" {
		t.Errorf("system_prompt after update = %q, want v2", got.SystemPrompt)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
}

func TestGetAgentByDomain(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	if _, err := s.UpsertAgent(ctx, Agent{Domain: "legal", Version: 1, Status: AgentStatusDraft, SystemPrompt: "legal agent"}); err != nil {
		t.Fatalf("setup UpsertAgent: %v", err)
	}

	// Unscoped lookup (teamID="") — should find any matching agent.
	got, err := s.GetAgentByDomain(ctx, "legal", "")
	if err != nil {
		t.Fatalf("GetAgentByDomain: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent, got nil")
	}
	if got.Domain != "legal" {
		t.Errorf("domain = %q, want legal", got.Domain)
	}
}

func TestGetAgentByDomain_NotFound(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	got, err := s.GetAgentByDomain(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing domain, got %+v", got)
	}
}

func TestListAgents(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	if _, err := s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusPublished, SystemPrompt: "a"}); err != nil {
		t.Fatalf("setup UpsertAgent (finance): %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "legal", Version: 1, Status: AgentStatusDraft, SystemPrompt: "b"}); err != nil {
		t.Fatalf("setup UpsertAgent (legal): %v", err)
	}

	agents, err := s.ListAgents(ctx, "")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("want 2 agents, got %d", len(agents))
	}
}

func TestPublishAgent(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, _ := s.UpsertAgent(ctx, Agent{Domain: "ops", Version: 1, Status: AgentStatusDraft, SystemPrompt: "ops"})

	if err := s.PublishAgent(ctx, id); err != nil {
		t.Fatalf("PublishAgent: %v", err)
	}
	got, _ := s.GetAgent(ctx, id)
	if got.Status != AgentStatusPublished {
		t.Errorf("status = %q, want published", got.Status)
	}
}

func TestStoreAndListAgentVersions(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	id, err := s.UpsertAgent(ctx, Agent{Domain: "finance", Version: 1, Status: AgentStatusDraft, SystemPrompt: "v1"})
	if err != nil {
		t.Fatalf("setup UpsertAgent: %v", err)
	}

	err = s.StoreAgentVersion(ctx, AgentVersion{
		AgentID:      id,
		Version:      1,
		SystemPrompt: "v1",
		Changelog:    "initial generation",
	})
	if err != nil {
		t.Fatalf("StoreAgentVersion: %v", err)
	}

	versions, err := s.ListAgentVersions(ctx, id)
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("want 1 version, got %d", len(versions))
	}
	if versions[0].Changelog != "initial generation" {
		t.Errorf("changelog = %q", versions[0].Changelog)
	}
	if versions[0].AgentID != id {
		t.Errorf("agent_id = %q, want %q", versions[0].AgentID, id)
	}
}

func TestListAgents_Empty(t *testing.T) {
	s := newTestAgentStore(t)
	agents, err := s.ListAgents(context.Background(), "")
	if err != nil {
		t.Fatalf("ListAgents on empty store: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("want 0 agents, got %d", len(agents))
	}
}

// TestUpsertAgentMultiTeamSameDomain proves that two teams with the same domain
// produce two distinct agent rows (one per team), and that neither overwrites
// the other. It also verifies the new (domain, team_id) uniqueness constraint
// and the migrateAgentsTeamUnique rebuild migration (run implicitly by NewSQLiteStore).
func TestUpsertAgentMultiTeamSameDomain(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	// Two teams, same domain.
	id1, err := s.UpsertAgent(ctx, Agent{
		Domain:       "finance",
		TeamID:       "team-A",
		Version:      1,
		Status:       AgentStatusDraft,
		SystemPrompt: "Team A finance agent",
	})
	if err != nil {
		t.Fatalf("UpsertAgent team-A: %v", err)
	}

	id2, err := s.UpsertAgent(ctx, Agent{
		Domain:       "finance",
		TeamID:       "team-B",
		Version:      1,
		Status:       AgentStatusDraft,
		SystemPrompt: "Team B finance agent",
	})
	if err != nil {
		t.Fatalf("UpsertAgent team-B: %v", err)
	}

	// IDs must be distinct.
	if id1 == id2 {
		t.Errorf("expected distinct IDs for different teams, both got %q", id1)
	}

	// Two rows total.
	all, err := s.ListAgents(ctx, "")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 agents, got %d", len(all))
	}

	// Each team's lookup returns its own agent.
	aA, err := s.GetAgentByDomain(ctx, "finance", "team-A")
	if err != nil {
		t.Fatalf("GetAgentByDomain team-A: %v", err)
	}
	if aA == nil || aA.ID != id1 {
		t.Errorf("team-A lookup: want id %q, got %v", id1, aA)
	}
	if aA.SystemPrompt != "Team A finance agent" {
		t.Errorf("team-A system_prompt = %q, want 'Team A finance agent'", aA.SystemPrompt)
	}

	aB, err := s.GetAgentByDomain(ctx, "finance", "team-B")
	if err != nil {
		t.Fatalf("GetAgentByDomain team-B: %v", err)
	}
	if aB == nil || aB.ID != id2 {
		t.Errorf("team-B lookup: want id %q, got %v", id2, aB)
	}
	if aB.SystemPrompt != "Team B finance agent" {
		t.Errorf("team-B system_prompt = %q, want 'Team B finance agent'", aB.SystemPrompt)
	}

	// Upserting again for team-A updates ONLY team-A's row.
	_, err = s.UpsertAgent(ctx, Agent{
		ID:           id1,
		Domain:       "finance",
		TeamID:       "team-A",
		Version:      2,
		Status:       AgentStatusDraft,
		SystemPrompt: "Team A finance agent v2",
	})
	if err != nil {
		t.Fatalf("UpsertAgent team-A v2: %v", err)
	}

	aAv2, _ := s.GetAgentByDomain(ctx, "finance", "team-A")
	if aAv2 == nil || aAv2.SystemPrompt != "Team A finance agent v2" {
		t.Errorf("after update team-A prompt = %q, want v2", aAv2.SystemPrompt)
	}

	// team-B row is unchanged.
	aBcheck, _ := s.GetAgentByDomain(ctx, "finance", "team-B")
	if aBcheck == nil || aBcheck.SystemPrompt != "Team B finance agent" {
		t.Errorf("team-B prompt should be unchanged, got %q", aBcheck.SystemPrompt)
	}

	// Still exactly 2 rows.
	all2, _ := s.ListAgents(ctx, "")
	if len(all2) != 2 {
		t.Errorf("want 2 agents after update, got %d", len(all2))
	}
}

// TestGetAgentByDomain_LegacyFallback verifies that a teamID-scoped lookup falls
// back to a legacy row (team_id="") when no exact-team match exists.
func TestGetAgentByDomain_LegacyFallback(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	// Insert a legacy row (no team).
	id, err := s.UpsertAgent(ctx, Agent{
		Domain:       "ops",
		TeamID:       "",
		Version:      1,
		Status:       AgentStatusPublished,
		SystemPrompt: "Legacy ops agent",
	})
	if err != nil {
		t.Fatalf("UpsertAgent legacy: %v", err)
	}

	// A scoped lookup for an unknown team should fall back to the legacy row.
	a, err := s.GetAgentByDomain(ctx, "ops", "new-team")
	if err != nil {
		t.Fatalf("GetAgentByDomain: %v", err)
	}
	if a == nil {
		t.Fatal("expected legacy fallback agent, got nil")
	}
	if a.ID != id {
		t.Errorf("fallback id = %q, want %q", a.ID, id)
	}

	// A scoped lookup for the same team after it registers its own agent returns
	// the team-specific one, not the legacy one.
	id2, err := s.UpsertAgent(ctx, Agent{
		Domain:       "ops",
		TeamID:       "new-team",
		Version:      1,
		Status:       AgentStatusDraft,
		SystemPrompt: "new-team ops agent",
	})
	if err != nil {
		t.Fatalf("UpsertAgent new-team: %v", err)
	}

	a2, err := s.GetAgentByDomain(ctx, "ops", "new-team")
	if err != nil {
		t.Fatalf("GetAgentByDomain new-team: %v", err)
	}
	if a2 == nil || a2.ID != id2 {
		t.Errorf("after team-specific upsert: want id %q, got %v", id2, a2)
	}
}

func TestRenameDomain_Cascades(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	// Target domain "engineering" on team t1: one entry, one cluster, one agent.
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{
		Type: KTPattern, Title: "Eng entry", Content: "c", Domain: "engineering",
		Author: "a", Team: "t1", TeamID: "t1",
	}, []float32{0.1, 0.2, 0.3, 0.4}); err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}
	if _, err := s.StoreCluster(ctx, Cluster{Domain: "engineering", Title: "Eng cluster", TeamID: "t1"}); err != nil {
		t.Fatalf("StoreCluster: %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "engineering", Version: 1, Status: AgentStatusDraft, TeamID: "t1"}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	// Control rows that must NOT change: same domain different team, and different domain same team.
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "engineering", Version: 1, Status: AgentStatusDraft, TeamID: "t2"}); err != nil {
		t.Fatalf("UpsertAgent control team: %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "legal", Version: 1, Status: AgentStatusDraft, TeamID: "t1"}); err != nil {
		t.Fatalf("UpsertAgent control domain: %v", err)
	}

	res, err := s.RenameDomain(ctx, "t1", "engineering", "Backend API Design")
	if err != nil {
		t.Fatalf("RenameDomain: %v", err)
	}
	if res.Entries != 1 || res.Clusters != 1 || res.Agents != 1 {
		t.Errorf("counts = %+v, want entries=1 clusters=1 agents=1", res)
	}

	// t1 agent now under the new domain.
	got, _ := s.GetAgentByDomain(ctx, "Backend API Design", "t1")
	if got == nil {
		t.Fatal("agent not found under new domain for t1")
	}
	// Control team t2 still under old domain.
	t2, _ := s.GetAgentByDomain(ctx, "engineering", "t2")
	if t2 == nil {
		t.Error("t2 agent should remain under old domain")
	}
}

func TestRenameDomain_ConflictRejected(t *testing.T) {
	s := newTestAgentStore(t)
	ctx := context.Background()

	if _, err := s.UpsertAgent(ctx, Agent{Domain: "engineering", Version: 1, Status: AgentStatusDraft, TeamID: "t1"}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "platform", Version: 1, Status: AgentStatusDraft, TeamID: "t1"}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	_, err := s.RenameDomain(ctx, "t1", "engineering", "platform")
	if err != ErrDomainExists {
		t.Fatalf("want ErrDomainExists, got %v", err)
	}
	// Original agent untouched.
	got, _ := s.GetAgentByDomain(ctx, "engineering", "t1")
	if got == nil {
		t.Error("engineering agent should be unchanged after rejected rename")
	}
}
