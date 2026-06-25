package mcp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/dsandor/memory/internal/enrich"
	"github.com/dsandor/memory/internal/live"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- fakeBus captures published events for assertions ---

type fakeBus struct {
	mu     sync.Mutex
	events []live.LiveEvent
}

func (f *fakeBus) Publish(ev live.LiveEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeBus) Subscribe(_ string, _ bool) (<-chan live.LiveEvent, func()) {
	ch := make(chan live.LiveEvent)
	return ch, func() { close(ch) }
}

func (f *fakeBus) snapshot() []live.LiveEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]live.LiveEvent, len(f.events))
	copy(out, f.events)
	return out
}

// --- mockStoreWithActivity extends mockStore to track RecordActivity calls ---

type mockStoreWithActivity struct {
	mockStore
	activities []storage.ActivityEvent
}

func (m *mockStoreWithActivity) RecordActivity(_ context.Context, ev storage.ActivityEvent) error {
	m.activities = append(m.activities, ev)
	return nil
}

// ---------------------------------------------------------------------------
// enrich_context tests
// ---------------------------------------------------------------------------

func TestHandleEnrichContext_PublishesLiveEvent_StdioFallback(t *testing.T) {
	store := &mockStoreWithActivity{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	bus := &fakeBus{}

	handler := internalmcp.HandleEnrichContext(store, newTestSources(embedder, nil), bus, enrich.EnrichDefaults{})
	req := callReq("prompt", "Analyse Q3 earnings for ACME Corp", "user", "alice", "team", "finance")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	evs := bus.snapshot()
	if len(evs) != 1 {
		t.Fatalf("expected 1 live event, got %d", len(evs))
	}
	ev := evs[0]

	if ev.Type != live.TypeEnrichContext {
		t.Errorf("event type = %q, want %q", ev.Type, live.TypeEnrichContext)
	}
	if ev.TeamID != "finance" {
		t.Errorf("teamID = %q, want %q", ev.TeamID, "finance")
	}
	if ev.Actor.ID != "alice" {
		t.Errorf("actor.ID = %q, want alice", ev.Actor.ID)
	}
	if ev.Fragment == "" {
		t.Error("expected non-empty fragment")
	}
	if ev.ID == "" {
		t.Error("expected event ID to be filled")
	}
	if ev.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be filled")
	}

	// Fragment must be capped.
	runes := []rune(ev.Fragment)
	if len(runes) > live.MaxFragmentLen {
		t.Errorf("fragment exceeds MaxFragmentLen: %d > %d", len(runes), live.MaxFragmentLen)
	}
}

func TestHandleEnrichContext_PublishesLiveEvent_LongPromptCapped(t *testing.T) {
	store := &mockStoreWithActivity{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	bus := &fakeBus{}

	longPrompt := ""
	for i := 0; i < live.MaxFragmentLen+50; i++ {
		longPrompt += "x"
	}

	handler := internalmcp.HandleEnrichContext(store, newTestSources(embedder, nil), bus, enrich.EnrichDefaults{})
	req := callReq("prompt", longPrompt)

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	evs := bus.snapshot()
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	runes := []rune(evs[0].Fragment)
	if len(runes) > live.MaxFragmentLen {
		t.Errorf("fragment not capped: got %d runes", len(runes))
	}
}

func TestHandleEnrichContext_NilBus_NoPublish_NoChange(t *testing.T) {
	store := &mockStoreWithActivity{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	handler := internalmcp.HandleEnrichContext(store, newTestSources(embedder, nil), nil, enrich.EnrichDefaults{})
	req := callReq("prompt", "test prompt with nil bus")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	// No panic, no change in behaviour.
}

func TestHandleEnrichContext_RecordsActivity(t *testing.T) {
	store := &mockStoreWithActivity{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	bus := &fakeBus{}

	handler := internalmcp.HandleEnrichContext(store, newTestSources(embedder, nil), bus, enrich.EnrichDefaults{})
	req := callReq("prompt", "what is the DCF model?")

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if len(store.activities) != 1 {
		t.Fatalf("expected 1 activity record, got %d", len(store.activities))
	}
	act := store.activities[0]
	if act.EventType != live.TypeEnrichContext {
		t.Errorf("activity event type = %q, want %q", act.EventType, live.TypeEnrichContext)
	}
	if act.Metadata["fragment"] == "" {
		t.Error("activity fragment metadata should not be empty")
	}
	if act.CreatedAt.IsZero() {
		t.Error("activity CreatedAt should not be zero")
	}
}

func TestHandleEnrichContext_StdioFallback_NoToolArgs(t *testing.T) {
	store := &mockStoreWithActivity{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	bus := &fakeBus{}

	handler := internalmcp.HandleEnrichContext(store, newTestSources(embedder, nil), bus, enrich.EnrichDefaults{})
	req := callReq("prompt", "plain prompt no user or team")

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	evs := bus.snapshot()
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].Actor.ID != "stdio" {
		t.Errorf("expected stdio actor, got %q", evs[0].Actor.ID)
	}
	if evs[0].TeamID != "" {
		t.Errorf("expected empty teamID for stdio, got %q", evs[0].TeamID)
	}
}

// ---------------------------------------------------------------------------
// knowledge_use tests
// ---------------------------------------------------------------------------

func TestHandleKnowledgeUse_PublishesLiveEvent(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "entry-1", Title: "DCF Model Prompt", TeamID: "finance"},
		},
	}
	bus := &fakeBus{}

	handler := internalmcp.HandleKnowledgeUse(store, bus)
	req := callReq("entry_id", "entry-1", "tool", "enrich_context")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	evs := bus.snapshot()
	if len(evs) != 1 {
		t.Fatalf("expected 1 live event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Type != live.TypeKnowledgeUsed {
		t.Errorf("event type = %q, want %q", ev.Type, live.TypeKnowledgeUsed)
	}
	if ev.EntryID != "entry-1" {
		t.Errorf("entryID = %q, want entry-1", ev.EntryID)
	}
	if ev.Title == "" {
		t.Error("expected title to be set from entry")
	}
}

func TestHandleKnowledgeUse_NilBus_NoPanic(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "entry-2", Title: "Test Entry", TeamID: "x"},
		},
	}

	handler := internalmcp.HandleKnowledgeUse(store, nil)
	req := callReq("entry_id", "entry-2", "tool", "knowledge_search")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
}

// ---------------------------------------------------------------------------
// knowledge_rate tests
// ---------------------------------------------------------------------------

func TestHandleKnowledgeRate_PublishesLiveEvent(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "entry-r1", Title: "Rate Me"},
		},
	}
	bus := &fakeBus{}

	handler := internalmcp.HandleKnowledgeRate(store, bus)
	req := callReq("id", "entry-r1", "rating", float64(4))

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	evs := bus.snapshot()
	if len(evs) != 1 {
		t.Fatalf("expected 1 live event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Type != live.TypeKnowledgeRated {
		t.Errorf("event type = %q, want %q", ev.Type, live.TypeKnowledgeRated)
	}
	if ev.EntryID != "entry-r1" {
		t.Errorf("entryID = %q, want entry-r1", ev.EntryID)
	}
	if ev.Meta["rating"] != "4" {
		t.Errorf("rating meta = %q, want 4", ev.Meta["rating"])
	}
}

func TestHandleKnowledgeRate_NilBus_NoPanic(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "entry-r2", Title: "Rate Me Too"},
		},
	}

	handler := internalmcp.HandleKnowledgeRate(store, nil)
	req := callReq("id", "entry-r2", "rating", float64(3))

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
}

// ---------------------------------------------------------------------------
// Compile-time interface check: fakeBus must satisfy live.EventBus
// ---------------------------------------------------------------------------

var _ live.EventBus = (*fakeBus)(nil)

// ---------------------------------------------------------------------------
// mockStoreWithActivity must satisfy storage.Store — all methods delegated
// to embedded mockStore except RecordActivity which is overridden above.
// ---------------------------------------------------------------------------

// Verify we can call GetUserByID on the mock (added in Task 2a) without panic.
// mockStore does not have GetUserByID — the auth.AuthStore interface requires it
// but storage.Store does not.  The mock only needs to satisfy storage.Store.
func TestMockStore_Satisfies_StorageStore(t *testing.T) {
	var _ storage.Store = (*mockStore)(nil)
	var _ storage.Store = (*mockStoreWithActivity)(nil)
}

// Compile-time check: callReq helper from tools_test.go resolves the mcplib import.
var _ mcplib.CallToolRequest
