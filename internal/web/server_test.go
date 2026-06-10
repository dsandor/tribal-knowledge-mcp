package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dsandor/memory/internal/storage"
	"github.com/dsandor/memory/internal/web"
)

// mockStore implements web.AllStore with configurable return values.
type mockStore struct {
	entries  []storage.KnowledgeEntry
	clusters []storage.Cluster
	agents   []storage.Agent
	versions []storage.AgentVersion
	snaps    []storage.DatasetSnapshot
	run      *storage.PipelineRun
}

// --- AgentStore / AnalysisStore / Store methods ---

func (m *mockStore) CountEntries(_ context.Context) (int, error) { return len(m.entries), nil }
func (m *mockStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return map[string][]float32{}, nil
}
func (m *mockStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) {
	return "x", nil
}
func (m *mockStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockStore) StartPipelineRun(_ context.Context, _ string) (string, error) {
	return "x", nil
}
func (m *mockStore) FinishPipelineRun(_ context.Context, _, _ string, _, _ int, _ []string) error {
	return nil
}
func (m *mockStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	return m.run, nil
}
func (m *mockStore) StoreSnapshot(_ context.Context, _ storage.DatasetSnapshot) (string, error) {
	return "x", nil
}
func (m *mockStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	if len(m.snaps) == 0 {
		return nil, nil
	}
	return &m.snaps[0], nil
}
func (m *mockStore) ListSnapshots(_ context.Context) ([]storage.DatasetSnapshot, error) {
	return m.snaps, nil
}
func (m *mockStore) StoreEntry(_ context.Context, _ storage.KnowledgeEntry, _ []float32) (string, error) {
	return "x", nil
}
func (m *mockStore) GetEntry(_ context.Context, id string) (*storage.KnowledgeEntry, error) {
	for i := range m.entries {
		if m.entries[i].ID == id {
			return &m.entries[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
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
func (m *mockStore) ApproveEntry(_ context.Context, _ string) error  { return nil }
func (m *mockStore) RejectEntry(_ context.Context, _ string) error   { return nil }
func (m *mockStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error { return nil }
func (m *mockStore) Ping(_ context.Context) error                     { return nil }
func (m *mockStore) Close() error                                     { return nil }
func (m *mockStore) UpsertAgent(_ context.Context, _ storage.Agent) (string, error) {
	return "x", nil
}
func (m *mockStore) GetAgent(_ context.Context, id string) (*storage.Agent, error) {
	for i := range m.agents {
		if m.agents[i].ID == id {
			return &m.agents[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (m *mockStore) GetAgentByDomain(_ context.Context, _ string) (*storage.Agent, error) {
	return nil, storage.ErrNotFound
}
func (m *mockStore) ListAgents(_ context.Context) ([]storage.Agent, error) { return m.agents, nil }
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
func (m *mockStore) DeleteTeam(_ context.Context, id string) error { return nil }
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
func (m *mockStore) RevokeAPIKey(_ context.Context, id string) error { return nil }
func (m *mockStore) TouchAPIKey(_ context.Context, id string) error  { return nil }
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
func (m *mockStore) PutAuthConfig(_ context.Context, c storage.AuthConfig) error { return nil }
func (m *mockStore) LogActivity(_ context.Context, e storage.ActivityEntry) error { return nil }
func (m *mockStore) QueryActivity(_ context.Context, teamID string, limit int) ([]storage.ActivityEntry, error) {
	return nil, nil
}
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
func (m *mockStore) ListPipelineRuns(_ context.Context, _ int) ([]storage.PipelineRun, error) {
	return nil, nil
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
		entries:  []storage.KnowledgeEntry{{ID: "e1"}, {ID: "e2"}},
		clusters: []storage.Cluster{{ID: "c1"}},
		agents:   []storage.Agent{{ID: "a1"}},
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
			{ID: "a1", Domain: "finance", Status: storage.AgentStatusPublished},
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

func TestHandleDatasetList(t *testing.T) {
	store := &mockStore{
		snaps: []storage.DatasetSnapshot{{ID: "s1", Version: 1, ClusterCount: 3, EntryCount: 10}},
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
