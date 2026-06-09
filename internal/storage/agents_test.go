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

	got, err := s.GetAgentByDomain(ctx, "legal")
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

	got, err := s.GetAgentByDomain(ctx, "nonexistent")
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

	agents, err := s.ListAgents(ctx)
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
	agents, err := s.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents on empty store: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("want 0 agents, got %d", len(agents))
	}
}
