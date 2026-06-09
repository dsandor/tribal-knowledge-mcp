package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// mockAgentStore embeds mockAnalysisStore (same package mcp_test) and adds AgentStore methods.
type mockAgentStore struct {
	mockAnalysisStore
	agents        []storage.Agent
	agentVersions []storage.AgentVersion
}

func (m *mockAgentStore) UpsertAgent(_ context.Context, a storage.Agent) (string, error) {
	if a.ID == "" {
		a.ID = "agent-" + a.Domain
	}
	for i, existing := range m.agents {
		if existing.Domain == a.Domain {
			m.agents[i] = a
			return a.ID, nil
		}
	}
	m.agents = append(m.agents, a)
	return a.ID, nil
}

func (m *mockAgentStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) GetAgentByDomain(_ context.Context, domain string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.Domain == domain {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) ListAgents(_ context.Context) ([]storage.Agent, error) {
	return m.agents, nil
}

func (m *mockAgentStore) PublishAgent(_ context.Context, id string) error {
	for i, a := range m.agents {
		if a.ID == id {
			m.agents[i].Status = storage.AgentStatusPublished
			return nil
		}
	}
	return nil
}

func (m *mockAgentStore) StoreAgentVersion(_ context.Context, v storage.AgentVersion) error {
	m.agentVersions = append(m.agentVersions, v)
	return nil
}

func (m *mockAgentStore) ListAgentVersions(_ context.Context, agentID string) ([]storage.AgentVersion, error) {
	var out []storage.AgentVersion
	for _, v := range m.agentVersions {
		if v.AgentID == agentID {
			out = append(out, v)
		}
	}
	return out, nil
}

func testAgentStoreWithData() *mockAgentStore {
	return &mockAgentStore{
		agents: []storage.Agent{
			{
				ID: "agent-finance", Domain: "finance", Version: 1,
				Status:       storage.AgentStatusPublished,
				SystemPrompt: "You are a finance agent.",
				Instructions: "Use DCF.",
				AntiPatterns: "No guessing.",
				SourceRefs:   []string{"cluster-1"},
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
			{
				ID: "agent-legal", Domain: "legal", Version: 1,
				Status:       storage.AgentStatusDraft,
				SystemPrompt: "You are a legal agent.",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			},
		},
	}
}

func TestHandleAgentList(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	var agents []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &agents); err != nil {
		t.Fatalf("parse agents JSON: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("want 2 agents, got %d", len(agents))
	}
}

func TestHandleAgentGet_ByID(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "finance") {
		t.Error("result should contain domain 'finance'")
	}
}

func TestHandleAgentGet_ByDomain(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq("domain", "legal"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "legal") {
		t.Error("result should contain domain 'legal'")
	}
}

func TestHandleAgentGet_MissingBoth(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentGet(store)
	result, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when neither id nor domain provided")
	}
}

func TestHandleAgentPublish(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentPublish(store)
	result, err := handler(context.Background(), callReq("id", "agent-legal"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	got, _ := store.GetAgent(context.Background(), "agent-legal")
	if got.Status != storage.AgentStatusPublished {
		t.Errorf("status = %q after publish, want published", got.Status)
	}
}

func TestHandleAgentExport_MD(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentExport(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance", "format", "md"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "---") {
		t.Error("md export should contain frontmatter")
	}
}

func TestHandleAgentExport_JSON(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentExport(store)
	result, err := handler(context.Background(), callReq("id", "agent-finance", "format", "json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &parsed); err != nil {
		t.Fatalf("json export not valid JSON: %v", err)
	}
	if parsed["domain"] != "finance" {
		t.Errorf("domain = %v, want finance", parsed["domain"])
	}
}

func TestHandleAgentExport_ByDomain(t *testing.T) {
	store := testAgentStoreWithData()
	handler := internalmcp.HandleAgentExport(store)
	result, err := handler(context.Background(), callReq("domain", "legal", "format", "txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error: %s", textContent(result))
	}
	if !strings.Contains(textContent(result), "legal") {
		t.Error("txt export should contain domain content for legal agent")
	}
}
