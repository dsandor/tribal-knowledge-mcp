package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- mock Store ---

type mockStore struct {
	entries   []storage.KnowledgeEntry
	storeErr  error
	listErr   error
	deleteErr error
}

func (m *mockStore) StoreEntry(_ context.Context, e storage.KnowledgeEntry, _ []float32) (string, error) {
	if m.storeErr != nil {
		return "", m.storeErr
	}
	e.ID = "mock-" + e.Title
	e.CreatedAt = time.Now()
	e.UpdatedAt = time.Now()
	e.Version = 1
	m.entries = append(m.entries, e)
	return e.ID, nil
}

func (m *mockStore) GetEntry(_ context.Context, id string) (*storage.KnowledgeEntry, error) {
	for _, e := range m.entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.entries, nil
}

func (m *mockStore) DeleteEntry(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockStore) SearchSimilar(_ context.Context, _ []float32, topK int) ([]storage.SearchResult, error) {
	results := make([]storage.SearchResult, 0, len(m.entries))
	for _, e := range m.entries {
		results = append(results, storage.SearchResult{Entry: e, Score: 0.9})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (m *mockStore) Close() error                                                  { return nil }
func (m *mockStore) RateEntry(_ context.Context, _ string, _ float64) error        { return nil }
func (m *mockStore) ApproveEntry(_ context.Context, _ string) error                { return nil }
func (m *mockStore) RejectEntry(_ context.Context, _ string) error                 { return nil }
func (m *mockStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error { return nil }
func (m *mockStore) Ping(_ context.Context) error                                  { return nil }
func (m *mockStore) RecordUsage(_ context.Context, _ storage.UsageEvent) error     { return nil }
func (m *mockStore) RecordOutcome(_ context.Context, _ storage.OutcomeRating) error { return nil }
func (m *mockStore) GetTrendingEntries(_ context.Context, _ string, _, _ int) ([]storage.TrendingEntry, error) {
	return nil, nil
}
func (m *mockStore) GetWeakSignalEntries(_ context.Context, _ string, _ int, _ float64) ([]storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockStore) RecordActivity(_ context.Context, _ storage.ActivityEvent) error { return nil }
func (m *mockStore) ListActivity(_ context.Context, _ string, _, _ int) ([]storage.ActivityEvent, error) {
	return nil, nil
}
func (m *mockStore) SearchHybrid(_ context.Context, _ string, _ string, _ []float32, _ string, _ int) ([]storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockStore) BulkImport(_ context.Context, _ []storage.KnowledgeEntry) (int, int, []string, error) {
	return 0, 0, nil, nil
}
func (m *mockStore) GetEntryByContentHash(_ context.Context, _ string) (*storage.KnowledgeEntry, error) {
	return nil, nil
}

// --- mock Embedder ---

type mockEmbedder struct {
	embedding []float32
	err       error
}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.embedding, m.err
}

// --- fake AI providers for constructing *aiconfig.Sources in tests ---

// fakeEmbedProvider always returns the same Embedder regardless of url/model.
type fakeEmbedProvider struct{ e embedding.Embedder }

func (f *fakeEmbedProvider) Embedder(_, _ string) embedding.Embedder { return f.e }

// fakeLLMProvider always returns the same Client regardless of apiKey/model.
type fakeLLMProvider struct{ c llm.Client }

func (f *fakeLLMProvider) Client(_, _ string) llm.Client { return f.c }

// fakeSettingsStore returns empty settings (no saved overrides).
type fakeSettingsStore struct{}

func (f *fakeSettingsStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	return &storage.TeamSettings{}, nil
}

// newTestSources builds a *aiconfig.Sources backed by the given embedder and
// optional LLM client. The resolver uses an empty settings store (env defaults
// only), which keeps nil-client behaviour for keys/URLs not provided.
func newTestSources(e embedding.Embedder, c llm.Client) *aiconfig.Sources {
	resolver := aiconfig.NewResolver(&fakeSettingsStore{}, aiconfig.EnvDefaults{
		// Provide non-empty key+model so the LLM provider is called when c != nil.
		AnthropicAPIKey: "test-key",
		AnthropicModel:  "test-model",
		OllamaURL:       "http://test-ollama",
		OllamaModel:     "test-model",
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         &fakeLLMProvider{c: c},
		Embed:       &fakeEmbedProvider{e: e},
		DefaultTeam: "test",
	}
}

// newNilEmbedSources builds a *aiconfig.Sources where OllamaURL is intentionally
// empty so that Sources.Embedder always returns nil, simulating an unconfigured
// embedding environment.
func newNilEmbedSources(c llm.Client) *aiconfig.Sources {
	resolver := aiconfig.NewResolver(&fakeSettingsStore{}, aiconfig.EnvDefaults{
		AnthropicAPIKey: "test-key",
		AnthropicModel:  "test-model",
		// OllamaURL deliberately omitted — Embedder will return nil.
		OllamaURL:   "",
		OllamaModel: "",
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         &fakeLLMProvider{c: c},
		Embed:       &fakeEmbedProvider{e: nil},
		DefaultTeam: "test",
	}
}

// --- helpers ---

func callReq(kv ...any) mcplib.CallToolRequest {
	req := mcplib.CallToolRequest{}
	args := make(map[string]any)
	for i := 0; i+1 < len(kv); i += 2 {
		args[kv[i].(string)] = kv[i+1]
	}
	req.Params.Arguments = args
	return req
}

func textContent(result *mcplib.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(mcplib.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

// --- tests ---

func TestHandleKnowledgeStore_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	req := callReq(
		"title", "Test Prompt",
		"content", "Use bullet points.",
		"type", "prompt",
		"domain", "general",
		"description", "Good for summaries",
		"tags", []any{"clarity", "bullets"},
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.entries))
	}
	if store.entries[0].Title != "Test Prompt" {
		t.Errorf("title: got %q, want %q", store.entries[0].Title, "Test Prompt")
	}
	if store.entries[0].Domain != "general" {
		t.Errorf("domain: got %q, want general", store.entries[0].Domain)
	}
	if len(store.entries[0].Tags) != 2 {
		t.Errorf("tags: got %v, want 2", store.entries[0].Tags)
	}
	// Response text should include the assigned ID
	responseText := textContent(result)
	if responseText == "" {
		t.Error("expected non-empty response text with entry ID")
	}
}

func TestHandleKnowledgeStore_NilEmbedder_ReturnsToolError(t *testing.T) {
	store := &mockStore{}

	// newNilEmbedSources yields a Sources whose Embedder always returns nil.
	handler := internalmcp.HandleKnowledgeStore(store, newNilEmbedSources(nil))
	req := callReq(
		"title", "Nil Embed Entry",
		"content", "Some content.",
		"type", "prompt",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when embedder is nil")
	}
	if len(store.entries) != 0 {
		t.Errorf("expected 0 stored entries when embedding is unconfigured, got %d", len(store.entries))
	}
}

func TestHandleKnowledgeSearch_NilEmbedder_ReturnsToolError(t *testing.T) {
	store := &mockStore{}

	// newNilEmbedSources yields a Sources whose Embedder always returns nil.
	handler := internalmcp.HandleKnowledgeSearch(store, newNilEmbedSources(nil))
	req := callReq("query", "any query text")

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when embedder is nil")
	}
}

func TestHandleKnowledgeStore_MissingRequired(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	req := callReq("title", "No Content") // missing content and type

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for missing required fields")
	}
}

func TestHandleKnowledgeGet_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	storeHandler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	storeResult, _ := storeHandler(context.Background(), callReq("title", "Alpha", "content", "c", "type", "prompt"))
	_ = storeResult

	id := store.entries[0].ID

	getHandler := internalmcp.HandleKnowledgeGet(store)
	result, err := getHandler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("get handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &entry); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if entry["Title"] != "Alpha" {
		t.Errorf("title: got %v, want Alpha", entry["Title"])
	}
}

func TestHandleKnowledgeGet_MissingID(t *testing.T) {
	store := &mockStore{}
	handler := internalmcp.HandleKnowledgeGet(store)
	result, err := handler(context.Background(), callReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when id is missing")
	}
}

func TestHandleKnowledgeList(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}
	storeHandler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))

	for _, title := range []string{"A", "B", "C"} {
		storeHandler(context.Background(), callReq("title", title, "content", "c", "type", "prompt"))
	}

	listHandler := internalmcp.HandleKnowledgeList(store)
	result, err := listHandler(context.Background(), callReq("limit", float64(10)))
	if err != nil {
		t.Fatalf("list handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	var entries []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &entries); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestHandleKnowledgeStore_PublishesLiveEvent(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}
	bus := &fakeBus{}

	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil), bus)
	req := callReq(
		"title", "Event Test",
		"content", "Some content.",
		"type", "prompt",
	)

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
	if ev.Type != live.TypeKnowledgeStored {
		t.Errorf("event type = %q, want %q", ev.Type, live.TypeKnowledgeStored)
	}
	if ev.Title != "Event Test" {
		t.Errorf("title = %q, want Event Test", ev.Title)
	}
	if ev.EntryID == "" {
		t.Error("expected non-empty EntryID")
	}
}

func TestHandleKnowledgeStore_NilBus_NoPanic(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	// No bus argument — should not panic.
	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	req := callReq(
		"title", "No Bus Entry",
		"content", "Content.",
		"type", "prompt",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
}

func TestHandleKnowledgeDelete_Success(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1}}

	storeHandler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	storeHandler(context.Background(), callReq("title", "ToDelete", "content", "c", "type", "prompt"))

	id := store.entries[0].ID

	deleteHandler := internalmcp.HandleKnowledgeDelete(store)
	result, err := deleteHandler(context.Background(), callReq("id", id))
	if err != nil {
		t.Fatalf("delete handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}
	if len(store.entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(store.entries))
	}
}
