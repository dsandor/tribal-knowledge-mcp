package pipeline

import (
	"context"

	"github.com/dsandor/memory/internal/storage"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

type mockAnalysisStore struct {
	entries    []storage.KnowledgeEntry
	embeddings map[string][]float32
	clusters   []storage.Cluster
	runs       []storage.PipelineRun
	snapshots  []storage.DatasetSnapshot
}

func (m *mockAnalysisStore) CountEntries(_ context.Context) (int, error) {
	return len(m.entries), nil
}
func (m *mockAnalysisStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return m.embeddings, nil
}
func (m *mockAnalysisStore) ListEntries(_ context.Context, _ storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	return m.entries, nil
}
func (m *mockAnalysisStore) StoreCluster(_ context.Context, c storage.Cluster) (string, error) {
	c.ID = "cluster-" + c.Title
	m.clusters = append(m.clusters, c)
	return c.ID, nil
}
func (m *mockAnalysisStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockAnalysisStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) StartPipelineRun(_ context.Context, trigger string) (string, error) {
	r := storage.PipelineRun{ID: "run-1", Status: "running", Trigger: trigger}
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
func (m *mockAnalysisStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	if len(m.runs) == 0 {
		return nil, nil
	}
	r := m.runs[len(m.runs)-1]
	return &r, nil
}
func (m *mockAnalysisStore) StoreSnapshot(_ context.Context, snap storage.DatasetSnapshot) (string, error) {
	snap.ID = "snap-1"
	m.snapshots = append(m.snapshots, snap)
	return snap.ID, nil
}
func (m *mockAnalysisStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	if len(m.snapshots) == 0 {
		return nil, nil
	}
	s := m.snapshots[len(m.snapshots)-1]
	return &s, nil
}
func (m *mockAnalysisStore) ListSnapshots(_ context.Context) ([]storage.DatasetSnapshot, error) {
	return m.snapshots, nil
}
func (m *mockAnalysisStore) RateEntry(_ context.Context, _ string, _ float64) error { return nil }

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
func (m *mockAnalysisStore) Close() error                                             { return nil }
func (m *mockAnalysisStore) ApproveEntry(_ context.Context, _ string) error           { return nil }
func (m *mockAnalysisStore) RejectEntry(_ context.Context, _ string) error            { return nil }
func (m *mockAnalysisStore) UpdateEntry(_ context.Context, _ storage.KnowledgeEntry) error { return nil }
func (m *mockAnalysisStore) Ping(_ context.Context) error { return nil }
func (m *mockAnalysisStore) RecordUsage(_ context.Context, _ storage.UsageEvent) error { return nil }
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
