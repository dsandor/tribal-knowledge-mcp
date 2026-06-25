package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/enrich"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
)

// ctxWithTeam returns a context carrying the given teamID as a TeamContext.
// UserID is set so that resolveActorTeam correctly identifies this as an
// authenticated request and returns the TeamID (not the stdio fallback).
func ctxWithTeam(teamID string) context.Context {
	return auth.WithTestTeamContext(context.Background(), auth.TeamContext{
		TeamID: teamID,
		UserID: "test-user-" + teamID,
		Role:   "member",
	})
}

// --- mockStoreTracking extends mockStore to record the last ListFilter and
// whether DeleteEntry / RateEntry were invoked.

type mockStoreTracking struct {
	mockStore
	lastFilter    storage.ListFilter
	deleteInvoked bool
	rateInvoked   bool
}

func (m *mockStoreTracking) ListEntries(_ context.Context, f storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	m.lastFilter = f
	return m.mockStore.entries, m.mockStore.listErr
}

func (m *mockStoreTracking) DeleteEntry(ctx context.Context, id string) error {
	m.deleteInvoked = true
	return m.mockStore.DeleteEntry(ctx, id)
}

func (m *mockStoreTracking) RateEntry(_ context.Context, _ string, _ float64) error {
	m.rateInvoked = true
	return nil
}

// ---------------------------------------------------------------------------
// TestKnowledgeStoreStampsTeamID
// ---------------------------------------------------------------------------

func TestKnowledgeStoreStampsTeamID(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	ctx := ctxWithTeam("t1")
	req := callReq("title", "TeamStamp", "content", "stamped content", "type", "prompt")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.entries))
	}
	if store.entries[0].TeamID != "t1" {
		t.Errorf("TeamID = %q, want %q", store.entries[0].TeamID, "t1")
	}
}

// ---------------------------------------------------------------------------
// TestKnowledgeListScopedByTeam
// ---------------------------------------------------------------------------

func TestKnowledgeListScopedByTeam(t *testing.T) {
	store := &mockStoreTracking{}

	handler := internalmcp.HandleKnowledgeList(store)
	ctx := ctxWithTeam("t1")
	req := callReq("limit", float64(10))

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if store.lastFilter.TeamID != "t1" {
		t.Errorf("ListFilter.TeamID = %q, want %q", store.lastFilter.TeamID, "t1")
	}
}

// ---------------------------------------------------------------------------
// TestKnowledgeGetForbidden
// ---------------------------------------------------------------------------

func TestKnowledgeGetForbidden(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "Secret", TeamID: "t2"},
		},
	}
	handler := internalmcp.HandleKnowledgeGet(store)

	// Cross-team → forbidden
	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "e1"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cross-team get")
	}
	if !strings.Contains(textContent(result), "forbidden") {
		t.Errorf("expected 'forbidden' in error text, got: %s", textContent(result))
	}
}

func TestKnowledgeGetAllowed_SameTeam(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e2", Title: "Mine", TeamID: "t1"},
		},
	}
	handler := internalmcp.HandleKnowledgeGet(store)

	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "e2"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for same-team get, got: %s", textContent(result))
	}
}

func TestKnowledgeGetAllowed_EmptyCtx(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e3", Title: "Any", TeamID: "t2"},
		},
	}
	handler := internalmcp.HandleKnowledgeGet(store)

	// No team ctx (stdio MCP) → allowed
	result, err := handler(context.Background(), callReq("id", "e3"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success with empty ctx, got: %s", textContent(result))
	}
}

// ---------------------------------------------------------------------------
// TestKnowledgeDeleteForbidden
// ---------------------------------------------------------------------------

func TestKnowledgeDeleteForbidden(t *testing.T) {
	store := &mockStoreTracking{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{
				{ID: "del1", Title: "Other Team Entry", TeamID: "t2"},
			},
		},
	}
	handler := internalmcp.HandleKnowledgeDelete(store)

	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "del1"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cross-team delete")
	}
	if !strings.Contains(textContent(result), "forbidden") {
		t.Errorf("expected 'forbidden' in error text, got: %s", textContent(result))
	}
	if store.deleteInvoked {
		t.Error("DeleteEntry should not have been called for forbidden request")
	}
}

// ---------------------------------------------------------------------------
// TestKnowledgeRateForbidden
// ---------------------------------------------------------------------------

func TestKnowledgeRateForbidden(t *testing.T) {
	store := &mockStoreTracking{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{
				{ID: "rate1", Title: "Other Team Entry", TeamID: "t2"},
			},
		},
	}
	handler := internalmcp.HandleKnowledgeRate(store, nil)

	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "rate1", "rating", float64(5)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cross-team rate")
	}
	if !strings.Contains(textContent(result), "forbidden") {
		t.Errorf("expected 'forbidden' in error text, got: %s", textContent(result))
	}
	if store.rateInvoked {
		t.Error("RateEntry should not have been called for forbidden request")
	}
}

// ---------------------------------------------------------------------------
// TestKnowledgeSearchFiltersByTeam
// ---------------------------------------------------------------------------

func TestKnowledgeSearchFiltersByTeam(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "s1", Title: "T1 Entry", TeamID: "t1", Domain: "x"},
			{ID: "s2", Title: "T2 Entry", TeamID: "t2", Domain: "x"},
		},
	}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	handler := internalmcp.HandleKnowledgeSearch(store, newTestSources(embedder, nil))
	ctx := ctxWithTeam("t1")
	req := callReq("query", "test query")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	out := textContent(result)
	if !strings.Contains(out, "T1 Entry") {
		t.Errorf("expected T1 Entry in output, got: %s", out)
	}
	if strings.Contains(out, "T2 Entry") {
		t.Errorf("expected T2 Entry to be filtered out, got: %s", out)
	}
}

func TestKnowledgeDeleteAllowed_SameTeam(t *testing.T) {
	store := &mockStoreTracking{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{
				{ID: "del2", Title: "Own Team Entry", TeamID: "t1"},
			},
		},
	}
	handler := internalmcp.HandleKnowledgeDelete(store)

	result, err := handler(ctxWithTeam("t1"), callReq("id", "del2"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for same-team delete, got: %s", textContent(result))
	}
	if !store.deleteInvoked {
		t.Error("DeleteEntry should have been called for same-team request")
	}
}

// ---------------------------------------------------------------------------
// TestEnrichContextFiltersByTeam
// ---------------------------------------------------------------------------

// TestEnrichContextFiltersByTeam: SearchSimilar returns entries from t1 and t2;
// caller is team t1 → relevant_knowledge must contain only the t1 entry.
func TestEnrichContextFiltersByTeam(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "k1", Title: "T1 Knowledge", TeamID: "t1"},
			{ID: "k2", Title: "T2 Knowledge", TeamID: "t2"},
		},
	}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}
	src := newTestSources(embedder, nil)

	handler := internalmcp.HandleEnrichContext(store, src, nil, enrich.EnrichDefaults{})
	ctx := ctxWithTeam("t1")
	req := callReq("prompt", "what is the best approach for financial analysis?")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	// Parse the JSON response and inspect relevant_knowledge.
	var resp struct {
		RelevantKnowledge []struct {
			Title string `json:"title"`
		} `json:"relevant_knowledge"`
	}
	raw := textContent(result)
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse response JSON: %v\nraw: %s", err, raw)
	}

	for _, k := range resp.RelevantKnowledge {
		if k.Title == "T2 Knowledge" {
			t.Errorf("relevant_knowledge must NOT contain T2 Knowledge (cross-team); got: %s", raw)
		}
	}
	found := false
	for _, k := range resp.RelevantKnowledge {
		if k.Title == "T1 Knowledge" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("relevant_knowledge must contain T1 Knowledge; got: %s", raw)
	}
}

// ---------------------------------------------------------------------------
// TestPromptSuggestFiltersByTeam
// ---------------------------------------------------------------------------

// TestPromptSuggestFiltersByTeam: SearchSimilar returns entries from t1 and t2;
// caller is team t1 → source_entries used must NOT include t2 entries.
func TestPromptSuggestFiltersByTeam(t *testing.T) {
	// Use Rating >= 3.5 on t1 entry so it ends up in topEntries.
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "p1", Title: "T1 Prompt", TeamID: "t1", Rating: 4.0},
			{ID: "p2", Title: "T2 Prompt", TeamID: "t2", Rating: 4.0},
		},
	}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}
	// We need an LLM client that just echoes something recognisable.
	llmClient := &fakeLLMClient{resp: `{"improved_prompt":"improved","rationale":"ok"}`}
	src := newTestSources(embedder, llmClient)

	handler := internalmcp.HandlePromptSuggest(store, src)
	ctx := ctxWithTeam("t1")
	req := callReq("prompt", "analyze the stock market trends")

	result, err := handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	out := textContent(result)
	// Source entries are listed as "Source entries used: id1, id2"
	if strings.Contains(out, "p2") {
		t.Errorf("response must NOT reference t2 entry id p2; got: %s", out)
	}
	if !strings.Contains(out, "p1") {
		t.Errorf("response should reference t1 entry id p1; got: %s", out)
	}
}

// fakeLLMClient is a minimal llm.Client that returns a canned response.
type fakeLLMClient struct{ resp string }

func (f *fakeLLMClient) Complete(_ context.Context, _ string) (string, error) { return f.resp, nil }

// ---------------------------------------------------------------------------
// TestAgentGetForbiddenMCP
// ---------------------------------------------------------------------------

// TestAgentGetForbiddenMCP: agent belongs to team t2; caller is t1 → forbidden error.
func TestAgentGetForbiddenMCP(t *testing.T) {
	store := &mockAgentStore{
		agents: []storage.Agent{
			{ID: "a1", Domain: "finance", TeamID: "t2"},
		},
	}
	handler := internalmcp.HandleAgentGet(store)
	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "a1"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cross-team agent get")
	}
	if !strings.Contains(textContent(result), "forbidden") {
		t.Errorf("expected 'forbidden' in error text, got: %s", textContent(result))
	}
}

// ---------------------------------------------------------------------------
// TestAgentPublishForbiddenMCP
// ---------------------------------------------------------------------------

// mockAgentStoreTracking extends mockAgentStore and records whether PublishAgent was called.
type mockAgentStoreTracking struct {
	mockAgentStore
	publishCalled bool
}

func (m *mockAgentStoreTracking) PublishAgent(ctx context.Context, id string) error {
	m.publishCalled = true
	return m.mockAgentStore.PublishAgent(ctx, id)
}

// TestAgentPublishForbiddenMCP: agent belongs to team t2; caller is t1 →
// forbidden error AND PublishAgent must NOT be called.
func TestAgentPublishForbiddenMCP(t *testing.T) {
	store := &mockAgentStoreTracking{
		mockAgentStore: mockAgentStore{
			agents: []storage.Agent{
				{ID: "a1", Domain: "finance", TeamID: "t2"},
			},
		},
	}
	handler := internalmcp.HandleAgentPublish(store)
	ctx := ctxWithTeam("t1")
	result, err := handler(ctx, callReq("id", "a1"))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for cross-team agent publish")
	}
	if !strings.Contains(textContent(result), "forbidden") {
		t.Errorf("expected 'forbidden' in error text, got: %s", textContent(result))
	}
	if store.publishCalled {
		t.Error("PublishAgent must NOT be called for forbidden cross-team request")
	}
}
