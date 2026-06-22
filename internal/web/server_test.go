package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// mockStore implements web.AllStore with configurable return values.
type mockStore struct {
	entries    []storage.KnowledgeEntry
	clusters   []storage.Cluster
	agents     []storage.Agent
	versions   []storage.AgentVersion
	snaps      []storage.DatasetSnapshot
	run        *storage.PipelineRun
	lastFilter storage.ListFilter // captured by ListEntries
}

// --- AgentStore / AnalysisStore / Store methods ---

func (m *mockStore) CountEntries(_ context.Context, teamID string) (int, error) {
	if teamID == "" {
		return len(m.entries), nil
	}
	count := 0
	for _, e := range m.entries {
		if e.TeamID == teamID {
			count++
		}
	}
	return count, nil
}
func (m *mockStore) GetAllEmbeddings(_ context.Context, _ string) (map[string][]float32, error) {
	return map[string][]float32{}, nil
}
func (m *mockStore) ListClusters(_ context.Context, teamID string) ([]storage.Cluster, error) {
	if teamID == "" {
		return m.clusters, nil
	}
	var out []storage.Cluster
	for _, c := range m.clusters {
		if c.TeamID == teamID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *mockStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) {
	return "x", nil
}
func (m *mockStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockStore) StartPipelineRun(_ context.Context, _, _ string) (string, error) {
	return "x", nil
}
func (m *mockStore) FinishPipelineRun(_ context.Context, _, _ string, _, _ int, _ []string) error {
	return nil
}
func (m *mockStore) GetLatestPipelineRun(_ context.Context, _ string) (*storage.PipelineRun, error) {
	return m.run, nil
}
func (m *mockStore) StoreSnapshot(_ context.Context, _ storage.DatasetSnapshot) (string, error) {
	return "x", nil
}
func (m *mockStore) GetLatestSnapshot(_ context.Context, teamID string) (*storage.DatasetSnapshot, error) {
	for i := range m.snaps {
		if teamID == "" || m.snaps[i].TeamID == teamID {
			return &m.snaps[i], nil
		}
	}
	return nil, nil
}
func (m *mockStore) ListSnapshots(_ context.Context, teamID string) ([]storage.DatasetSnapshot, error) {
	if teamID == "" {
		return m.snaps, nil
	}
	var out []storage.DatasetSnapshot
	for _, s := range m.snaps {
		if s.TeamID == teamID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *mockStore) StoreEntry(_ context.Context, _ storage.KnowledgeEntry, _ []float32) (string, error) {
	return "x", nil
}
func (m *mockStore) StoreEntryChunked(_ context.Context, _ storage.KnowledgeEntry, _ []storage.EntryChunk) (string, error) {
	return "x", nil
}
func (m *mockStore) ReplaceEntryChunks(_ context.Context, _ string, _ []storage.EntryChunk) error {
	return nil
}
func (m *mockStore) GetEntry(_ context.Context, id string) (*storage.KnowledgeEntry, error) {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return &m.entries[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListEntries(_ context.Context, f storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	m.lastFilter = f
	return m.entries, nil
}
func (m *mockStore) DeleteEntry(_ context.Context, _ string) error { return nil }
func (m *mockStore) SearchSimilar(_ context.Context, _ []float32, _ int) ([]storage.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) RateEntry(_ context.Context, id string, _ float64) error {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) ApproveEntry(_ context.Context, id string) error {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) RejectEntry(_ context.Context, id string) error {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error { return nil }
func (m *mockStore) UpdateAutoTags(_ context.Context, _ string, _ []string) error  { return nil }
func (m *mockStore) Ping(_ context.Context) error                                  { return nil }
func (m *mockStore) Close() error                                                  { return nil }
func (m *mockStore) UpsertAgent(_ context.Context, _ storage.Agent) (string, error) {
	return "x", nil
}
func (m *mockStore) RenameDomain(_ context.Context, _, _, _ string) (storage.RenameDomainResult, error) {
	return storage.RenameDomainResult{}, nil
}
func (m *mockStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for i := range m.agents {
		if m.agents[i].ID == id {
			return &m.agents[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) GetAgentByDomain(_ context.Context, _, _ string) (*storage.Agent, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListAgents(_ context.Context, teamID string) ([]storage.Agent, error) {
	if teamID == "" {
		return m.agents, nil
	}
	var out []storage.Agent
	for _, a := range m.agents {
		if a.TeamID == teamID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *mockStore) PublishAgent(_ context.Context, id string) error {
	for i := range m.agents {
		if m.agents[i].ID == id {
			m.agents[i].Status = storage.AgentStatusPublished
			return nil
		}
	}
	return storage.ErrNotFound
}
func (m *mockStore) StoreAgentVersion(_ context.Context, _ storage.AgentVersion) error { return nil }
func (m *mockStore) ListAgentVersions(_ context.Context, _ string) ([]storage.AgentVersion, error) {
	return m.versions, nil
}

// --- TeamStore stubs ---

func (m *mockStore) CreateTeam(_ context.Context, t storage.Team) (string, error) {
	return "", nil
}
func (m *mockStore) GetTeam(_ context.Context, id string) (*storage.Team, error) { return nil, nil }
func (m *mockStore) ListTeams(_ context.Context) ([]storage.Team, error)         { return nil, nil }
func (m *mockStore) SetTeamEnabled(_ context.Context, id string, enabled bool) error {
	return nil
}
func (m *mockStore) UpdateTeam(_ context.Context, _ string, _ string, _ []string) error { return nil }
func (m *mockStore) DeleteTeam(_ context.Context, id string) error                      { return nil }
func (m *mockStore) TeamDataCounts(_ context.Context, _ string) (storage.TeamDataCounts, error) {
	return storage.TeamDataCounts{}, nil
}
func (m *mockStore) DeleteTeamMigrate(_ context.Context, _, _ string) (storage.TeamMigrationSummary, error) {
	return storage.TeamMigrationSummary{}, nil
}
func (m *mockStore) UpsertUser(_ context.Context, u storage.User) (string, error) {
	return "", nil
}
func (m *mockStore) GetUserByID(_ context.Context, id string) (*storage.User, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStore) GetUserByEmail(_ context.Context, email string) (*storage.User, error) {
	return nil, nil
}
func (m *mockStore) GetUserByExternalID(_ context.Context, externalID string) (*storage.User, error) {
	return nil, nil
}
func (m *mockStore) ListUsers(_ context.Context, teamID string) ([]storage.User, error) {
	return nil, nil
}
func (m *mockStore) AssignUserToTeam(_ context.Context, userID, teamID, role string) error {
	return nil
}
func (m *mockStore) AutoAssignUserToTeam(_ context.Context, userID, teamID, role string) error {
	return nil
}
func (m *mockStore) ResolveTeamByEmail(_ context.Context, email string) (*storage.Team, error) {
	return nil, nil
}
func (m *mockStore) CreateAPIKey(_ context.Context, key storage.APIKey) error { return nil }

// GetAPIKeyByHash returns a valid admin API key for any hash — allows tests to pass auth middleware.
func (m *mockStore) GetAPIKeyByHash(_ context.Context, hash string) (*storage.APIKey, error) {
	return &storage.APIKey{ID: "test-key", TeamID: "test-team", Role: "admin", KeyHash: hash}, nil
}
func (m *mockStore) ListAPIKeys(_ context.Context, teamID string) ([]storage.APIKey, error) {
	return nil, nil
}
func (m *mockStore) RevokeAPIKey(_ context.Context, id string) error          { return nil }
func (m *mockStore) TouchAPIKey(_ context.Context, id string) error           { return nil }
func (m *mockStore) CreateSession(_ context.Context, s storage.Session) error { return nil }
func (m *mockStore) GetSession(_ context.Context, tokenHash string) (*storage.Session, error) {
	return nil, nil
}
func (m *mockStore) DeleteSession(_ context.Context, tokenHash string) error { return nil }
func (m *mockStore) GetTeamSettings(_ context.Context, teamID string) (*storage.TeamSettings, error) {
	return &storage.TeamSettings{}, nil
}
func (m *mockStore) PutTeamSettings(_ context.Context, s storage.TeamSettings) error { return nil }
func (m *mockStore) GetAuthConfig(_ context.Context) (*storage.AuthConfig, error) {
	return &storage.AuthConfig{Provider: "local"}, nil
}
func (m *mockStore) PutAuthConfig(_ context.Context, c storage.AuthConfig) error  { return nil }
func (m *mockStore) LogActivity(_ context.Context, e storage.ActivityEntry) error { return nil }
func (m *mockStore) QueryActivity(_ context.Context, teamID string, limit int) ([]storage.ActivityEntry, error) {
	return nil, nil
}
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
func (m *mockStore) ListPipelineRuns(_ context.Context, _ string, _ int) ([]storage.PipelineRun, error) {
	return nil, nil
}
func (m *mockStore) BackfillTeamID(_ context.Context, _ string) error { return nil }
func (m *mockStore) AddVisibilityRule(_ context.Context, _, _, _ string) (storage.VisibilityRule, error) {
	return storage.VisibilityRule{}, nil
}
func (m *mockStore) DeleteVisibilityRule(_ context.Context, _, _, _ string) error { return nil }
func (m *mockStore) ListVisibilityRules(_ context.Context, _ string) ([]storage.VisibilityRule, error) {
	return nil, nil
}
func (m *mockStore) MarkInterruptedRuns(_ context.Context) (int, error) { return 0, nil }
func (m *mockStore) GetAnalysisCache(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (m *mockStore) PutAnalysisCache(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockStore) PruneAnalysisCache(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}

// Compile-time check that *mockStore satisfies web.AllStore.
var _ web.AllStore = (*mockStore)(nil)

func newTestServer(t *testing.T, store *mockStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store)
}

// authRequest wraps httptest.NewRequest and adds a Bearer token so requests
// pass the RequireAuth middleware (mockStore accepts any hash).
func authRequest(method, target, body string) *http.Request {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, bodyReader)
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestHandleStats(t *testing.T) {
	store := &mockStore{
		entries:  []storage.KnowledgeEntry{{ID: "e1", TeamID: "test-team"}, {ID: "e2", TeamID: "test-team"}},
		clusters: []storage.Cluster{{ID: "c1", TeamID: "test-team"}},
		agents:   []storage.Agent{{ID: "a1", TeamID: "test-team"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/stats", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var stats web.StatsResponse
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.KnowledgeCount != 2 {
		t.Errorf("KnowledgeCount = %d, want 2", stats.KnowledgeCount)
	}
	if stats.ClusterCount != 1 {
		t.Errorf("ClusterCount = %d, want 1", stats.ClusterCount)
	}
	if stats.AgentCount != 1 {
		t.Errorf("AgentCount = %d, want 1", stats.AgentCount)
	}
	if stats.PipelineStatus != "idle" {
		t.Errorf("PipelineStatus = %q, want idle", stats.PipelineStatus)
	}
}

func TestHandleKnowledgeList(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "Entry One", Type: "prompt"},
			{ID: "e2", Title: "Entry Two", Type: "pattern"},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge?limit=10", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var entries []storage.KnowledgeEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

func TestHandleKnowledgeGet_Found(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", Title: "Found Entry"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/e1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var entry storage.KnowledgeEntry
	if err := json.NewDecoder(w.Body).Decode(&entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Title != "Found Entry" {
		t.Errorf("title = %q, want Found Entry", entry.Title)
	}
}

func TestHandleKnowledgeGet_NotFound(t *testing.T) {
	srv := newTestServer(t, &mockStore{})
	req := authRequest("GET", "/api/knowledge/missing", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleKnowledgeRate(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("PUT", "/api/knowledge/e1/rate", `{"rating":4.5}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("response ok = %v, want true", resp["ok"])
	}
}

func TestHandleKnowledgeRate_InvalidRating(t *testing.T) {
	store := &mockStore{entries: []storage.KnowledgeEntry{{ID: "e1"}}}
	srv := newTestServer(t, store)
	req := authRequest("PUT", "/api/knowledge/e1/rate", `{"rating":9.9}`)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandleAgentList(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{
			{ID: "a1", Domain: "finance", Status: storage.AgentStatusPublished, TeamID: "test-team"},
		},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var agents []storage.Agent
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("want 1 agent, got %d", len(agents))
	}
}

func TestHandleAgentGet_Found(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/a1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Agent    storage.Agent          `json:"agent"`
		Versions []storage.AgentVersion `json:"versions"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Agent.Domain != "finance" {
		t.Errorf("agent.Domain = %q, want finance", resp.Agent.Domain)
	}
	if resp.Versions == nil {
		t.Errorf("versions should be non-nil (empty slice)")
	}
}

func TestHandleAgentPublish(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance", Status: storage.AgentStatusDraft}},
	}
	srv := newTestServer(t, store)
	// handleAgentPublish is now a curator route — our mock returns role "admin" which satisfies curator check
	req := authRequest("PUT", "/api/agents/a1/publish", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("response ok = %v, want true", resp["ok"])
	}
	if store.agents[0].Status != storage.AgentStatusPublished {
		t.Errorf("agent status = %q, want published", store.agents[0].Status)
	}
}

func TestHandleAgentExport_MD(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance", SystemPrompt: "You are a finance agent."}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/a1/export?format=md", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("Content-Type = %q, want text/markdown", ct)
	}
	if !strings.Contains(w.Body.String(), "finance") {
		t.Errorf("export body missing domain name")
	}
}

func TestHandleStaticFallback(t *testing.T) {
	srv := newTestServer(t, &mockStore{})
	// Static routes do not require auth
	req := httptest.NewRequest("GET", "/agents/some-id", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("SPA fallback: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "app") {
		t.Errorf("SPA fallback should serve index.html content")
	}
}

// capturingStore embeds mockStore and captures the most recent StoreEntry call.
// The mutex guards against async auto-tagging goroutines in tests that wire
// AI sources; only StoreEntry and entry() touch lastEntry.
type capturingStore struct {
	mockStore
	mu        sync.Mutex
	lastEntry storage.KnowledgeEntry
}

func (c *capturingStore) StoreEntry(_ context.Context, e storage.KnowledgeEntry, _ []float32) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastEntry = e
	return "captured-id", nil
}

func (c *capturingStore) entry() storage.KnowledgeEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastEntry
}

// TestKnowledgeStoreExtractsHashtags verifies that POST /api/knowledge merges
// explicit tags with hashtags extracted from title+content (deduped, lowercased).
func TestKnowledgeStoreExtractsHashtags(t *testing.T) {
	store := &capturingStore{}
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, store)

	body := `{"title":"T","content":"check #alpha now","type":"pattern","tags":["beta"]}`
	req := authRequest("POST", "/api/knowledge", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	got := store.entry().Tags
	want := []string{"beta", "alpha"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i, tag := range want {
		if got[i] != tag {
			t.Errorf("tags[%d] = %q, want %q", i, got[i], tag)
		}
	}
}

func TestHandleDatasetList(t *testing.T) {
	store := &mockStore{
		snaps: []storage.DatasetSnapshot{{ID: "s1", Version: 1, ClusterCount: 3, EntryCount: 10, TeamID: "test-team"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/datasets", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var snaps []storage.DatasetSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snaps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("want 1 snapshot, got %d", len(snaps))
	}
}

// TestKnowledgeListTagParam asserts that GET /api/knowledge?tag=alpha passes
// Tag:"alpha" through to the store's ListFilter (SQL-level filtering).
func TestKnowledgeListTagParam(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "Alpha Entry", Tags: []string{"alpha"}},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge?tag=alpha", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.lastFilter.Tag != "alpha" {
		t.Errorf("ListFilter.Tag = %q, want %q", store.lastFilter.Tag, "alpha")
	}
}

// TestKnowledgeExportTagParam asserts that GET /api/knowledge/export?tag=beta
// passes Tag:"beta" to the store (SQL-level filter, no post-hoc loop).
func TestKnowledgeExportTagParam(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "e1", Title: "Beta Entry", Tags: []string{"beta"}},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/export?tag=beta", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.lastFilter.Tag != "beta" {
		t.Errorf("export ListFilter.Tag = %q, want %q", store.lastFilter.Tag, "beta")
	}
}

// TestKnowledgeExportCSVAutoTagsColumn asserts that CSV export includes an
// auto_tags column immediately after tags, pipe-separated.
func TestKnowledgeExportCSVAutoTagsColumn(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{
				ID:       "e1",
				Title:    "Test Entry",
				Tags:     []string{"alpha", "beta"},
				AutoTags: []string{"cat1", "cat2"},
				Status:   "approved",
			},
		},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/export?format=csv", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 CSV lines, got %d: %s", len(lines), body)
	}

	// Verify header contains auto_tags immediately after tags
	header := lines[0]
	if !strings.Contains(header, "auto_tags") {
		t.Errorf("CSV header missing auto_tags column: %s", header)
	}
	// tags must appear before auto_tags (word-bounded to avoid matching
	// the "tags" substring inside "auto_tags")
	tagsIdx := strings.Index(header, ",tags,")
	autoTagsIdx := strings.Index(header, ",auto_tags,")
	if tagsIdx < 0 || autoTagsIdx < 0 || tagsIdx >= autoTagsIdx {
		t.Errorf("auto_tags must come after tags in header: %s", header)
	}

	// Verify data row includes auto_tags value
	if !strings.Contains(lines[1], "cat1|cat2") {
		t.Errorf("CSV data row missing auto_tags value (cat1|cat2): %s", lines[1])
	}
}

// --- Team isolation tests ---

// trackingStore wraps mockStore and records whether each mutation method was called.
type trackingStore struct {
	mockStore
	mu            sync.Mutex
	updateCalled  bool
	deleteCalled  bool
	rateCalled    bool
	approveCalled bool
	rejectCalled  bool
}

func (t *trackingStore) UpdateEntry(ctx context.Context, e storage.KnowledgeEntry) error {
	t.mu.Lock()
	t.updateCalled = true
	t.mu.Unlock()
	return t.mockStore.UpdateEntry(ctx, e)
}

func (t *trackingStore) DeleteEntry(ctx context.Context, id string) error {
	t.mu.Lock()
	t.deleteCalled = true
	t.mu.Unlock()
	return t.mockStore.DeleteEntry(ctx, id)
}

func (t *trackingStore) RateEntry(ctx context.Context, id string, rating float64) error {
	t.mu.Lock()
	t.rateCalled = true
	t.mu.Unlock()
	return t.mockStore.RateEntry(ctx, id, rating)
}

func (t *trackingStore) ApproveEntry(ctx context.Context, id string) error {
	t.mu.Lock()
	t.approveCalled = true
	t.mu.Unlock()
	return t.mockStore.ApproveEntry(ctx, id)
}

func (t *trackingStore) RejectEntry(ctx context.Context, id string) error {
	t.mu.Lock()
	t.rejectCalled = true
	t.mu.Unlock()
	return t.mockStore.RejectEntry(ctx, id)
}

// newTrackingServer creates a test server backed by a trackingStore.
func newTrackingServer(t *testing.T, store *trackingStore) *web.Server {
	t.Helper()
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	return web.NewServer(staticFS, store)
}

// TestKnowledgeListScopedByTeam verifies that GET /api/knowledge injects the
// caller's TeamID into the ListFilter.
func TestKnowledgeListScopedByTeam(t *testing.T) {
	// mockStore.GetAPIKeyByHash returns TeamID:"test-team" for any bearer token.
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "test-team"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.lastFilter.TeamID != "test-team" {
		t.Errorf("ListFilter.TeamID = %q, want %q", store.lastFilter.TeamID, "test-team")
	}
}

// TestKnowledgeGetForbiddenCrossTeam verifies that GET /api/knowledge/{id}
// returns 403 when the entry belongs to a different team.
func TestKnowledgeGetForbiddenCrossTeam(t *testing.T) {
	// Caller is "test-team" (from mockStore.GetAPIKeyByHash), entry is "t2".
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/e1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestKnowledgeGetSameTeamOK verifies that GET /api/knowledge/{id} succeeds
// when the caller and entry are on the same team.
func TestKnowledgeGetSameTeamOK(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "test-team"}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/e1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestKnowledgeGetLegacyEmptyTeamOK verifies that an entry with no TeamID is
// always accessible regardless of the caller's team (legacy data compatibility).
func TestKnowledgeGetLegacyEmptyTeamOK(t *testing.T) {
	store := &mockStore{
		entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: ""}},
	}
	srv := newTestServer(t, store)

	req := authRequest("GET", "/api/knowledge/e1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestKnowledgeUpdateForbiddenCrossTeam verifies that PUT /api/knowledge/{id}
// returns 403 and does NOT call UpdateEntry when the entry belongs to another team.
func TestKnowledgeUpdateForbiddenCrossTeam(t *testing.T) {
	store := &trackingStore{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
		},
	}
	srv := newTrackingServer(t, store)

	req := authRequest("PUT", "/api/knowledge/e1", `{"title":"x"}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
	store.mu.Lock()
	called := store.updateCalled
	store.mu.Unlock()
	if called {
		t.Error("UpdateEntry must NOT be called for cross-team entry")
	}
}

// TestKnowledgeDeleteForbiddenCrossTeam verifies that DELETE /api/knowledge/{id}
// returns 403 and does NOT call DeleteEntry when the entry belongs to another team.
func TestKnowledgeDeleteForbiddenCrossTeam(t *testing.T) {
	store := &trackingStore{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
		},
	}
	srv := newTrackingServer(t, store)

	req := authRequest("DELETE", "/api/knowledge/e1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	store.mu.Lock()
	called := store.deleteCalled
	store.mu.Unlock()
	if called {
		t.Error("DeleteEntry must NOT be called for cross-team entry")
	}
}

// TestKnowledgeRateForbiddenCrossTeam verifies that PUT /api/knowledge/{id}/rate
// returns 403 and does NOT call RateEntry when the entry belongs to another team.
func TestKnowledgeRateForbiddenCrossTeam(t *testing.T) {
	store := &trackingStore{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
		},
	}
	srv := newTrackingServer(t, store)

	req := authRequest("PUT", "/api/knowledge/e1/rate", `{"rating":4.0}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	store.mu.Lock()
	called := store.rateCalled
	store.mu.Unlock()
	if called {
		t.Error("RateEntry must NOT be called for cross-team entry")
	}
}

// TestKnowledgeApproveForbiddenCrossTeam verifies that PUT /api/knowledge/{id}/approve
// returns 403 and does NOT call ApproveEntry when the entry belongs to another team.
func TestKnowledgeApproveForbiddenCrossTeam(t *testing.T) {
	store := &trackingStore{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
		},
	}
	srv := newTrackingServer(t, store)

	req := authRequest("PUT", "/api/knowledge/e1/approve", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	store.mu.Lock()
	called := store.approveCalled
	store.mu.Unlock()
	if called {
		t.Error("ApproveEntry must NOT be called for cross-team entry")
	}
}

// TestKnowledgeRejectForbiddenCrossTeam verifies that PUT /api/knowledge/{id}/reject
// returns 403 and does NOT call RejectEntry when the entry belongs to another team.
func TestKnowledgeRejectForbiddenCrossTeam(t *testing.T) {
	store := &trackingStore{
		mockStore: mockStore{
			entries: []storage.KnowledgeEntry{{ID: "e1", TeamID: "t2"}},
		},
	}
	srv := newTrackingServer(t, store)

	req := authRequest("PUT", "/api/knowledge/e1/reject", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	store.mu.Lock()
	called := store.rejectCalled
	store.mu.Unlock()
	if called {
		t.Error("RejectEntry must NOT be called for cross-team entry")
	}
}

// --- Task 5: cluster/agent/dataset by-ID ownership checks ---

// publishAgentTrackingStore extends trackingStore and records whether PublishAgent was called.
type publishAgentTrackingStore struct {
	trackingStore
	mu            sync.Mutex
	publishCalled bool
}

func (p *publishAgentTrackingStore) PublishAgent(ctx context.Context, id string) error {
	p.mu.Lock()
	p.publishCalled = true
	p.mu.Unlock()
	return p.trackingStore.PublishAgent(ctx, id)
}

// TestAgentGetForbiddenCrossTeam: agent with TeamID "t2" → GET /api/agents/{id} → 403.
func TestAgentGetForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "finance", TeamID: "t2"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/a1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestAgentPublishForbiddenCrossTeam: agent TeamID "t2" → PUT /api/agents/{id}/publish → 403, PublishAgent not called.
func TestAgentPublishForbiddenCrossTeam(t *testing.T) {
	base := &publishAgentTrackingStore{}
	base.trackingStore.mockStore.agents = []storage.Agent{{ID: "a1", Domain: "finance", TeamID: "t2"}}
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, base)
	req := authRequest("PUT", "/api/agents/a1/publish", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	base.mu.Lock()
	called := base.publishCalled
	base.mu.Unlock()
	if called {
		t.Error("PublishAgent must NOT be called for cross-team agent")
	}
}

// TestClusterGetForbiddenCrossTeam: cluster TeamID "t2" → GET /api/clusters/{id} → 403.
func TestClusterGetForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		clusters: []storage.Cluster{{ID: "c1", Domain: "finance", TeamID: "t2"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/clusters/c1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestClusterSummaryForbiddenCrossTeam: cluster TeamID "t2" → GET /api/clusters/{id}/summary → 403.
func TestClusterSummaryForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		clusters: []storage.Cluster{{ID: "c1", Domain: "finance", TeamID: "t2", Summary: "some summary"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/clusters/c1/summary", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestDatasetExportForbiddenCrossTeam: snapshot TeamID "t2" → GET /api/datasets/{id}/export → 403.
func TestDatasetExportForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		snaps: []storage.DatasetSnapshot{{ID: "s1", Version: 1, TeamID: "t2"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/datasets/s1/export", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestAgentGetLegacyEmptyTeamOK: agent with TeamID "" → GET /api/agents/{id} → 200 (legacy compat).
func TestAgentGetLegacyEmptyTeamOK(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "legacy", TeamID: ""}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/a1", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200 for legacy empty-team agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAgentBulkExportFiltersCrossTeam: store holds one "test-team" agent + one "t2" agent;
// bulk export output must contain only the test-team agent.
func TestAgentBulkExportFiltersCrossTeam(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{
			{ID: "a1", Domain: "team-domain", TeamID: "test-team"},
			{ID: "a2", Domain: "other-domain", TeamID: "t2"},
		},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/bulk-export", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "team-domain") {
		t.Errorf("bulk export should contain test-team agent domain 'team-domain', body: %s", body)
	}
	if strings.Contains(body, "other-domain") {
		t.Errorf("bulk export must NOT contain cross-team agent domain 'other-domain', body: %s", body)
	}
}

// TestAgentExportForbiddenCrossTeam: single-agent export for a "t2" agent → GET /api/agents/{id}/export → 403.
func TestAgentExportForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "cross-domain", TeamID: "t2"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/a1/export", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestAgentRefactorForbiddenCrossTeam: refactor route for a "t2" agent → POST /api/agents/{id}/refactor → 403.
// The ownership check now runs before the aiSrc nil guard, so even with no AI configured a
// cross-team caller receives 403, not 503.
func TestAgentRefactorForbiddenCrossTeam(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{{ID: "a1", Domain: "cross-domain", TeamID: "t2"}},
	}
	srv := newTestServer(t, store)
	req := authRequest("POST", "/api/agents/a1/refactor", `{"feedback":"some feedback text here"}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// domainAgentStore overrides GetAgentByDomain to return a specific agent,
// enabling tests that target the /api/agents/domain/{domain}/latest endpoint.
type domainAgentStore struct {
	mockStore
	domainAgent *storage.Agent
}

func (d *domainAgentStore) GetAgentByDomain(_ context.Context, _, _ string) (*storage.Agent, error) {
	if d.domainAgent == nil {
		return nil, storage.ErrNotFound
	}
	return d.domainAgent, nil
}

// TestAgentLatestByDomainForbiddenCrossTeam: agent with TeamID "t2" →
// GET /api/agents/domain/{domain}/latest → 403 for a "test-team" caller.
func TestAgentLatestByDomainForbiddenCrossTeam(t *testing.T) {
	store := &domainAgentStore{
		domainAgent: &storage.Agent{ID: "a1", Domain: "finance", TeamID: "t2", Status: storage.AgentStatusPublished},
	}
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>app</body></html>"), Mode: 0444, ModTime: time.Now()},
	}
	srv := web.NewServer(staticFS, store)
	req := authRequest("GET", "/api/agents/domain/finance/latest", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "forbidden") {
		t.Errorf("body should contain 'forbidden': %s", w.Body.String())
	}
}

// TestAgentBulkExportIncludesLegacyAgents: store holds one "test-team" agent, one "t2" agent, and
// one legacy "" agent → bulk export must contain test-team AND legacy agents, but NOT the t2 one.
func TestAgentBulkExportIncludesLegacyAgents(t *testing.T) {
	store := &mockStore{
		agents: []storage.Agent{
			{ID: "a1", Domain: "team-domain", TeamID: "test-team"},
			{ID: "a2", Domain: "other-domain", TeamID: "t2"},
			{ID: "a3", Domain: "legacy-domain", TeamID: ""},
		},
	}
	srv := newTestServer(t, store)
	req := authRequest("GET", "/api/agents/bulk-export", "")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "team-domain") {
		t.Errorf("bulk export should contain test-team agent domain 'team-domain', body: %s", body)
	}
	if !strings.Contains(body, "legacy-domain") {
		t.Errorf("bulk export should contain legacy (empty-team) agent domain 'legacy-domain', body: %s", body)
	}
	if strings.Contains(body, "other-domain") {
		t.Errorf("bulk export must NOT contain cross-team agent domain 'other-domain', body: %s", body)
	}
}
