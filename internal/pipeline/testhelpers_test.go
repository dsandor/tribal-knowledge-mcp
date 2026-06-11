package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

type mockLLM struct {
	response string
	err      error
	calls    int
	mu       sync.Mutex
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return m.response, m.err
}

func (m *mockLLM) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockAISource implements AISource for tests. analysisClient, agentClient, and
// improvementClient can each be set independently; nil means "not configured".
// fingerprint is returned by LLMFingerprint; defaults to "" (empty string).
type mockAISource struct {
	analysisClient    llm.Client
	agentClient       llm.Client
	improvementClient llm.Client
	fingerprint       string
}

func (s *mockAISource) AnalysisLLM(_ context.Context, _ string) llm.Client { return s.analysisClient }
func (s *mockAISource) AgentLLM(_ context.Context, _ string) llm.Client    { return s.agentClient }
func (s *mockAISource) ImprovementLLM(_ context.Context, _ string) llm.Client {
	return s.improvementClient
}
func (s *mockAISource) LLMFingerprint(_ context.Context, _, _ string) string { return s.fingerprint }

// newSrc is a convenience constructor for a mockAISource where the analysis and
// improvement clients use the same mock (the common case in existing tests).
func newSrc(llmMock llm.Client) *mockAISource {
	return &mockAISource{analysisClient: llmMock, improvementClient: llmMock}
}

// newSrcWithAgent sets all three clients independently.
func newSrcWithAgent(analysis, agentLLM, improvement llm.Client) *mockAISource {
	return &mockAISource{analysisClient: analysis, agentClient: agentLLM, improvementClient: improvement}
}

type mockAnalysisStore struct {
	entries    []storage.KnowledgeEntry
	embeddings map[string][]float32
	clusters   []storage.Cluster
	runs       []storage.PipelineRun
	snapshots  []storage.DatasetSnapshot

	// functional in-memory analysis cache (lazily initialized)
	cacheMu     sync.Mutex
	cacheData   map[string]string
	pruneCalled time.Duration
	prunedCount int
	putCacheErr error // if non-nil, PutAnalysisCache returns this error
}

func (m *mockAnalysisStore) CountEntries(_ context.Context, teamID string) (int, error) {
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
func (m *mockAnalysisStore) GetAllEmbeddings(_ context.Context, teamID string) (map[string][]float32, error) {
	if teamID == "" {
		return m.embeddings, nil
	}
	// filter by teamID — not wired to entries here; return all for now
	return m.embeddings, nil
}
func (m *mockAnalysisStore) ListTeams(_ context.Context) ([]storage.Team, error) {
	return nil, nil
}
func (m *mockAnalysisStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	return m.entries, nil
}
func (m *mockAnalysisStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) {
	c.ID = "cluster-" + c.Title
	m.clusters = append(m.clusters, c)
	return c.ID, nil
}
func (m *mockAnalysisStore) ListClusters(_ context.Context, teamID string) ([]storage.Cluster, error) {
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
func (m *mockAnalysisStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) StartPipelineRun(_ context.Context, trigger, teamID string) (string, error) {
	r := storage.PipelineRun{ID: "run-1", Status: "running", Trigger: trigger, TeamID: teamID}
	m.runs = append(m.runs, r)
	return "run-1", nil
}
func (m *mockAnalysisStore) FinishPipelineRun(_ context.Context, id, status string, ep, cf int, errs []string) error {
	for i := range m.runs {
		if m.runs[i].ID == id {
			m.runs[i].Status = status
			m.runs[i].EntriesProcessed = ep
			m.runs[i].ClustersFound = cf
			m.runs[i].Errors = errs
		}
	}
	return nil
}
func (m *mockAnalysisStore) GetLatestPipelineRun(_ context.Context, teamID string) (*storage.PipelineRun, error) {
	for i := len(m.runs) - 1; i >= 0; i-- {
		if teamID == "" || m.runs[i].TeamID == teamID {
			r := m.runs[i]
			return &r, nil
		}
	}
	return nil, nil
}
func (m *mockAnalysisStore) StoreSnapshot(_ context.Context, snap storage.DatasetSnapshot) (string, error) {
	snap.ID = "snap-1"
	m.snapshots = append(m.snapshots, snap)
	return snap.ID, nil
}
func (m *mockAnalysisStore) GetLatestSnapshot(_ context.Context, teamID string) (*storage.DatasetSnapshot, error) {
	for i := len(m.snapshots) - 1; i >= 0; i-- {
		if teamID == "" || m.snapshots[i].TeamID == teamID {
			s := m.snapshots[i]
			return &s, nil
		}
	}
	return nil, nil
}
func (m *mockAnalysisStore) ListSnapshots(_ context.Context, teamID string) ([]storage.DatasetSnapshot, error) {
	if teamID == "" {
		return m.snapshots, nil
	}
	var out []storage.DatasetSnapshot
	for _, s := range m.snapshots {
		if s.TeamID == teamID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *mockAnalysisStore) RateEntry(_ context.Context, _ string, _ float64) error { return nil }
func (m *mockAnalysisStore) GetEntryByContentHash(_ context.Context, _ string) (*storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) ListPipelineRuns(_ context.Context, teamID string, limit int) ([]storage.PipelineRun, error) {
	if teamID == "" {
		return m.runs, nil
	}
	var out []storage.PipelineRun
	for _, r := range m.runs {
		if r.TeamID == teamID {
			out = append(out, r)
		}
	}
	return out, nil
}

// Store interface stubs — not exercised in pipeline tests.
func (m *mockAnalysisStore) StoreEntry(_ context.Context, _ storage.KnowledgeEntry, _ []float32) (string, error) {
	return "", nil
}
func (m *mockAnalysisStore) GetEntry(_ context.Context, _ string) (*storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) DeleteEntry(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) SearchSimilar(_ context.Context, _ []float32, _ int) ([]storage.SearchResult, error) {
	return nil, nil
}
func (m *mockAnalysisStore) Close() error                                   { return nil }
func (m *mockAnalysisStore) ApproveEntry(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) RejectEntry(_ context.Context, _ string) error  { return nil }
func (m *mockAnalysisStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error {
	return nil
}
func (m *mockAnalysisStore) UpdateAutoTags(_ context.Context, _ string, _ []string) error { return nil }
func (m *mockAnalysisStore) Ping(_ context.Context) error                                 { return nil }
func (m *mockAnalysisStore) RecordUsage(_ context.Context, _ storage.UsageEvent) error    { return nil }
func (m *mockAnalysisStore) RecordOutcome(_ context.Context, _ storage.OutcomeRating) error {
	return nil
}
func (m *mockAnalysisStore) GetTrendingEntries(_ context.Context, _ string, _, _ int) ([]storage.TrendingEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) GetWeakSignalEntries(_ context.Context, _ string, _ int, _ float64) ([]storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) RecordActivity(_ context.Context, _ storage.ActivityEvent) error {
	return nil
}
func (m *mockAnalysisStore) ListActivity(_ context.Context, _ string, _, _ int) ([]storage.ActivityEvent, error) {
	return nil, nil
}
func (m *mockAnalysisStore) SearchHybrid(_ context.Context, _ string, _ string, _ []float32, _ string, _ int) ([]storage.KnowledgeEntry, error) {
	return nil, nil
}
func (m *mockAnalysisStore) BulkImport(_ context.Context, _ []storage.KnowledgeEntry) (int, int, []string, error) {
	return 0, 0, nil, nil
}
func (m *mockAnalysisStore) BackfillTeamID(_ context.Context, _ string) error   { return nil }
func (m *mockAnalysisStore) MarkInterruptedRuns(_ context.Context) (int, error) { return 0, nil }
func (m *mockAnalysisStore) GetAnalysisCache(_ context.Context, kind, key string) (string, bool, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cacheData == nil {
		return "", false, nil
	}
	v, ok := m.cacheData[kind+"|"+key]
	return v, ok, nil
}
func (m *mockAnalysisStore) PutAnalysisCache(_ context.Context, kind, key, value, _ string) error {
	if m.putCacheErr != nil {
		return m.putCacheErr
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cacheData == nil {
		m.cacheData = make(map[string]string)
	}
	m.cacheData[kind+"|"+key] = value
	return nil
}
func (m *mockAnalysisStore) PruneAnalysisCache(_ context.Context, olderThan time.Duration) (int, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.pruneCalled = olderThan
	return m.prunedCount, nil
}

// cacheKey builds the map key used internally (exported for test clarity).
func (m *mockAnalysisStore) cacheKey(kind, key string) string {
	return fmt.Sprintf("%s|%s", kind, key)
}

type mockAgentStore struct {
	mockAnalysisStore
	agents        []storage.Agent
	agentVersions []storage.AgentVersion
}

func (m *mockAgentStore) UpsertAgent(_ context.Context, a storage.Agent) (string, error) {
	if a.ID == "" {
		a.ID = "agent-" + a.Domain + "-" + a.TeamID
	}
	// Key on (domain, team_id) — mirrors the real store's per-team semantics.
	for i, existing := range m.agents {
		if existing.Domain == a.Domain && existing.TeamID == a.TeamID {
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

// GetAgentByDomain mirrors the real store's team-scoped lookup semantics:
//   - teamID non-empty: exact (domain, team_id) match first; fallback to legacy
//     row with team_id="" if no exact match.
//   - teamID empty: returns any agent with matching domain (dev fallback).
func (m *mockAgentStore) GetAgentByDomain(_ context.Context, domain, teamID string) (*storage.Agent, error) {
	if teamID == "" {
		for i := range m.agents {
			if m.agents[i].Domain == domain {
				return &m.agents[i], nil
			}
		}
		return nil, nil
	}
	// Exact match.
	for i := range m.agents {
		if m.agents[i].Domain == domain && m.agents[i].TeamID == teamID {
			return &m.agents[i], nil
		}
	}
	// Legacy fallback (team_id="").
	for i := range m.agents {
		if m.agents[i].Domain == domain && m.agents[i].TeamID == "" {
			return &m.agents[i], nil
		}
	}
	return nil, nil
}

func (m *mockAgentStore) ListAgents(_ context.Context, teamID string) ([]storage.Agent, error) {
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

func (m *mockAgentStore) PublishAgent(_ context.Context, _ string) error { return nil }

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
