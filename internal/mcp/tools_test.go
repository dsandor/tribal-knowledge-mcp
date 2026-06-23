package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- mock Store ---

type mockStore struct {
	entries    []storage.KnowledgeEntry
	storeErr   error
	listErr    error
	deleteErr  error
	lastChunks []storage.EntryChunk // captured from the most recent StoreEntryChunked call
	// visRules maps a user id to that user's suppression rules (used by the
	// per-user visibility filter tests). nil for all other users.
	visRules map[string][]storage.VisibilityRule
	// users maps a user id to a stored User (for owner-identity resolution).
	users map[string]storage.User
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

func (m *mockStore) StoreEntryChunked(ctx context.Context, e storage.KnowledgeEntry, chunks []storage.EntryChunk) (string, error) {
	m.lastChunks = chunks
	var emb []float32
	if len(chunks) > 0 {
		emb = chunks[0].Embedding
	}
	return m.StoreEntry(ctx, e, emb)
}

func (m *mockStore) ReplaceEntryChunks(_ context.Context, _ string, _ []storage.EntryChunk) error {
	return nil
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

func (m *mockStore) Close() error                                                   { return nil }
func (m *mockStore) RateEntry(_ context.Context, _ string, _ float64) error         { return nil }
func (m *mockStore) ApproveEntry(_ context.Context, _ string) error                 { return nil }
func (m *mockStore) RejectEntry(_ context.Context, _ string) error                  { return nil }
func (m *mockStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error  { return nil }
func (m *mockStore) UpdateAutoTags(_ context.Context, _ string, _ []string) error   { return nil }
func (m *mockStore) Ping(_ context.Context) error                                   { return nil }
func (m *mockStore) RecordUsage(_ context.Context, _ storage.UsageEvent) error      { return nil }
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
func (m *mockStore) BackfillTeamID(_ context.Context, _ string) error { return nil }
func (m *mockStore) ReassignEntriesTeam(_ context.Context, _ []string, _ string) error {
	return nil
}
func (m *mockStore) AddVisibilityRule(_ context.Context, userID, ruleType, value string) (storage.VisibilityRule, error) {
	if m.visRules == nil {
		m.visRules = map[string][]storage.VisibilityRule{}
	}
	for _, r := range m.visRules[userID] {
		if r.RuleType == ruleType && r.Value == value {
			return r, nil // idempotent
		}
	}
	r := storage.VisibilityRule{
		ID:        "vis-" + userID + "-" + ruleType + "-" + value,
		UserID:    userID,
		RuleType:  ruleType,
		Value:     value,
		CreatedAt: time.Now(),
	}
	m.visRules[userID] = append(m.visRules[userID], r)
	return r, nil
}
func (m *mockStore) DeleteVisibilityRule(_ context.Context, userID, ruleType, value string) error {
	rules := m.visRules[userID]
	for i, r := range rules {
		if r.RuleType == ruleType && r.Value == value {
			m.visRules[userID] = append(rules[:i], rules[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *mockStore) ListVisibilityRules(_ context.Context, userID string) ([]storage.VisibilityRule, error) {
	return m.visRules[userID], nil
}
func (m *mockStore) CreateShare(_ context.Context, _, _, _ string) (storage.KnowledgeShare, error) {
	return storage.KnowledgeShare{}, nil
}
func (m *mockStore) GetShare(_ context.Context, _ string) (*storage.KnowledgeShare, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStore) MarkShareUsed(_ context.Context, _, _, _ string) error { return nil }
func (m *mockStore) RevokeShare(_ context.Context, _ string) error         { return nil }

// GetUserByID lets the visibility helper resolve owner identities. The base
// storage.Store interface does not require it; the helper type-asserts for it.
func (m *mockStore) GetUserByID(_ context.Context, id string) (*storage.User, error) {
	if u, ok := m.users[id]; ok {
		return &u, nil
	}
	return nil, storage.ErrNotFound
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
func (f *fakeLLMProvider) Ollama(_, _ string) llm.Client { return f.c }

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

// newChunkedTestSources builds a *aiconfig.Sources like newTestSources but with
// a small embedding token budget so content larger than maxTokens is split into
// multiple chunks by the store path.
func newChunkedTestSources(e embedding.Embedder, maxTokens int) *aiconfig.Sources {
	resolver := aiconfig.NewResolver(&fakeSettingsStore{}, aiconfig.EnvDefaults{
		AnthropicAPIKey:    "test-key",
		AnthropicModel:     "test-model",
		OllamaURL:          "http://test-ollama",
		OllamaModel:        "test-model",
		EmbeddingMaxTokens: maxTokens,
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         &fakeLLMProvider{c: nil},
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
	if got := store.entries[0].Tags; len(got) != 2 || got[0] != "clarity" || got[1] != "bullets" {
		t.Errorf("tags: got %v, want [clarity bullets]", got)
	}
	// Response text should include the assigned ID
	responseText := textContent(result)
	if responseText == "" {
		t.Error("expected non-empty response text with entry ID")
	}
}

func TestHandleKnowledgeStore_ChunksLargeContent(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	// Tiny token budget so multi-paragraph content must split. CountTokens is
	// runes/4 and the chunker applies a 0.9 safety fraction, so a budget of 10
	// tokens (~36 runes effective) forces several chunks for the content below.
	handler := internalmcp.HandleKnowledgeStore(store, newChunkedTestSources(embedder, 10))

	large := "First paragraph with enough words to matter here.\n\n" +
		"Second paragraph that also carries meaningful content for chunking.\n\n" +
		"Third paragraph rounding out the document so it clearly exceeds budget."
	req := callReq(
		"title", "Large Doc",
		"content", large,
		"type", "knowledge",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	if len(store.lastChunks) <= 1 {
		t.Fatalf("expected store to receive >1 chunk, got %d", len(store.lastChunks))
	}

	var res struct {
		ChunkCount         int `json:"chunk_count"`
		EmbeddingMaxTokens int `json:"embedding_max_tokens"`
	}
	if err := json.Unmarshal([]byte(textContent(result)), &res); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if res.ChunkCount <= 1 {
		t.Errorf("chunk_count: got %d, want > 1", res.ChunkCount)
	}
	if res.ChunkCount != len(store.lastChunks) {
		t.Errorf("chunk_count %d != chunks sent to store %d", res.ChunkCount, len(store.lastChunks))
	}
	if res.EmbeddingMaxTokens != 10 {
		t.Errorf("embedding_max_tokens: got %d, want 10", res.EmbeddingMaxTokens)
	}
}

func TestHandleKnowledgeStore_TinyContentSingleChunk(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	handler := internalmcp.HandleKnowledgeStore(store, newChunkedTestSources(embedder, 10))
	req := callReq(
		"title", "Tiny",
		"content", "Short.",
		"type", "knowledge",
	)

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %s", textContent(result))
	}

	if len(store.lastChunks) != 1 {
		t.Fatalf("expected exactly 1 chunk, got %d", len(store.lastChunks))
	}

	var res struct {
		ChunkCount int `json:"chunk_count"`
	}
	if err := json.Unmarshal([]byte(textContent(result)), &res); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if res.ChunkCount != 1 {
		t.Errorf("chunk_count: got %d, want 1", res.ChunkCount)
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

func TestKnowledgeStoreExtractsHashtags(t *testing.T) {
	store := &mockStore{}
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2}}

	handler := internalmcp.HandleKnowledgeStore(store, newTestSources(embedder, nil))
	req := callReq(
		"title", "Earnings workflow",
		"content", "Always check #earnings and #q3-report first",
		"type", "workflow",
		"tags", []any{"manual-tag", "earnings"},
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

	got := store.entries[0].Tags
	want := []string{"manual-tag", "earnings", "q3-report"}
	if len(got) != len(want) {
		t.Fatalf("tags: got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("tags[%d]: got %q, want %q", i, got[i], w)
		}
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
