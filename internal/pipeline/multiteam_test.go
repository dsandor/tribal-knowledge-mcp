package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

// teamTrackingAISource extends mockAISource by recording every teamID passed
// to AnalysisLLM so tests can assert per-team resolution.
type teamTrackingAISource struct {
	mockAISource
	mu      sync.Mutex
	teamIDs []string
}

func (s *teamTrackingAISource) AnalysisLLM(ctx context.Context, teamID string) llm.Client {
	s.mu.Lock()
	s.teamIDs = append(s.teamIDs, teamID)
	s.mu.Unlock()
	return s.mockAISource.AnalysisLLM(ctx, teamID)
}

func (s *teamTrackingAISource) recordedTeamIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.teamIDs))
	copy(out, s.teamIDs)
	return out
}

// multiteamStore wraps mockAnalysisStore and lets tests seed a teams list.
// It also supports injecting per-team ListEntries errors.
type multiteamStore struct {
	mockAnalysisStore
	teams          []storage.Team
	listEntriesErr map[string]error // teamID → error to return from ListEntries
	mu             sync.Mutex
	runsByTeam     map[string][]storage.PipelineRun
}

func (m *multiteamStore) ListTeams(_ context.Context) ([]storage.Team, error) {
	return m.teams, nil
}

func (m *multiteamStore) ListEntries(ctx context.Context, f storage.ListFilter) ([]storage.KnowledgeEntry, error) {
	if m.listEntriesErr != nil {
		if err, ok := m.listEntriesErr[f.TeamID]; ok {
			return nil, err
		}
	}
	// filter by teamID
	if f.TeamID == "" {
		return m.entries, nil
	}
	var out []storage.KnowledgeEntry
	for _, e := range m.entries {
		if e.TeamID == f.TeamID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *multiteamStore) GetAllEmbeddings(_ context.Context, teamID string) (map[string][]float32, error) {
	if teamID == "" {
		return m.embeddings, nil
	}
	// Build a filtered set: only embeddings for entries in the given team.
	result := make(map[string][]float32)
	for _, e := range m.entries {
		if e.TeamID == teamID {
			if emb, ok := m.embeddings[e.ID]; ok {
				result[e.ID] = emb
			}
		}
	}
	return result, nil
}

func (m *multiteamStore) StartPipelineRun(_ context.Context, trigger, teamID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runsByTeam == nil {
		m.runsByTeam = make(map[string][]storage.PipelineRun)
	}
	runID := "run-" + teamID + "-" + trigger
	r := storage.PipelineRun{ID: runID, Status: "running", Trigger: trigger, TeamID: teamID}
	m.runs = append(m.runs, r)
	m.runsByTeam[teamID] = append(m.runsByTeam[teamID], r)
	return runID, nil
}

func (m *multiteamStore) FinishPipelineRun(_ context.Context, id, status string, ep, cf int, errs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.runs {
		if m.runs[i].ID == id {
			m.runs[i].Status = status
			m.runs[i].EntriesProcessed = ep
			m.runs[i].ClustersFound = cf
			m.runs[i].Errors = errs
		}
	}
	for team := range m.runsByTeam {
		for i := range m.runsByTeam[team] {
			if m.runsByTeam[team][i].ID == id {
				m.runsByTeam[team][i].Status = status
			}
		}
	}
	return nil
}

func (m *multiteamStore) CountEntries(_ context.Context, teamID string) (int, error) {
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

func (m *multiteamStore) runsForTeam(teamID string) []storage.PipelineRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runsByTeam[teamID]
}

// TestRunIteratesTeams verifies that with two seeded teams, Run processes BOTH
// teams: AnalysisLLM is resolved for each team's ID, clusters are stamped with
// the correct TeamID, and each team gets its own pipeline run row.
func TestRunIteratesTeams(t *testing.T) {
	store := &multiteamStore{
		teams: []storage.Team{
			{ID: "t1", Enabled: true},
			{ID: "t2", Enabled: true},
		},
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "a1", Title: "Entry A1", Content: "Finance pattern", Domain: "finance", TeamID: "t1"},
				{ID: "a2", Title: "Entry A2", Content: "Finance workflow", Domain: "finance", TeamID: "t1"},
				{ID: "b1", Title: "Entry B1", Content: "Legal pattern", Domain: "legal", TeamID: "t2"},
				{ID: "b2", Title: "Entry B2", Content: "Legal workflow", Domain: "legal", TeamID: "t2"},
			},
			embeddings: map[string][]float32{
				"a1": {1, 0, 0, 0},
				"a2": {0.99, 0.14, 0, 0},
				"b1": {0, 0, 1, 0},
				"b2": {0, 0, 0.99, 0.14},
			},
		},
	}

	llmMock := &mockLLM{response: `{"title":"Cluster","summary":"A cluster.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	src := &teamTrackingAISource{
		mockAISource: mockAISource{analysisClient: llmMock, improvementClient: llmMock},
	}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// AnalysisLLM should have been called for both t1 and t2.
	resolved := src.recordedTeamIDs()
	hasT1, hasT2 := false, false
	for _, id := range resolved {
		if id == "t1" {
			hasT1 = true
		}
		if id == "t2" {
			hasT2 = true
		}
	}
	if !hasT1 {
		t.Errorf("AnalysisLLM was not called for team t1; resolved: %v", resolved)
	}
	if !hasT2 {
		t.Errorf("AnalysisLLM was not called for team t2; resolved: %v", resolved)
	}

	// Each team should have exactly one run row.
	if len(store.runsForTeam("t1")) != 1 {
		t.Errorf("want 1 run for t1, got %d", len(store.runsForTeam("t1")))
	}
	if len(store.runsForTeam("t2")) != 1 {
		t.Errorf("want 1 run for t2, got %d", len(store.runsForTeam("t2")))
	}

	// All clusters should have their respective team IDs stamped.
	store.cacheMu.Lock()
	clusters := append([]storage.Cluster{}, store.clusters...)
	store.cacheMu.Unlock()
	for _, c := range clusters {
		if c.TeamID != "t1" && c.TeamID != "t2" {
			t.Errorf("cluster %q has unexpected TeamID %q", c.Title, c.TeamID)
		}
	}
	t1Clusters := 0
	t2Clusters := 0
	for _, c := range clusters {
		if c.TeamID == "t1" {
			t1Clusters++
		}
		if c.TeamID == "t2" {
			t2Clusters++
		}
	}
	if t1Clusters == 0 {
		t.Error("expected at least 1 cluster for t1")
	}
	if t2Clusters == 0 {
		t.Error("expected at least 1 cluster for t2")
	}
}

// TestRunTeamFailureIsolated verifies that when one team's ListEntries returns
// an error, the other team still completes and gets a finished run row.
func TestRunTeamFailureIsolated(t *testing.T) {
	store := &multiteamStore{
		teams: []storage.Team{
			{ID: "t1", Enabled: true},
			{ID: "t2", Enabled: true},
		},
		listEntriesErr: map[string]error{
			"t1": errors.New("t1 store failure"),
		},
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "b1", Title: "Entry B1", Content: "Legal pattern", Domain: "legal", TeamID: "t2"},
				{ID: "b2", Title: "Entry B2", Content: "Legal workflow", Domain: "legal", TeamID: "t2"},
			},
			embeddings: map[string][]float32{
				"b1": {0, 0, 1, 0},
				"b2": {0, 0, 0.99, 0.14},
			},
		},
	}

	llmMock := &mockLLM{response: `{"title":"Legal","summary":"Legal entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	src := &mockAISource{analysisClient: llmMock, improvementClient: llmMock}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	// Run should NOT return an error even though t1 fails.
	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run should not propagate team failure, got: %v", err)
	}

	// t2 should have a completed run.
	t2Runs := store.runsForTeam("t2")
	if len(t2Runs) != 1 {
		t.Fatalf("want 1 run for t2, got %d", len(t2Runs))
	}
	if t2Runs[0].Status == "running" {
		t.Errorf("t2 run should be finished, not running")
	}

	// t1 run row should exist and be marked failed.
	t1Runs := store.runsForTeam("t1")
	if len(t1Runs) != 1 {
		t.Fatalf("want 1 run row for t1 (even on failure), got %d", len(t1Runs))
	}
	if t1Runs[0].Status != "failed" {
		t.Errorf("t1 run status = %q, want failed", t1Runs[0].Status)
	}
}

// TestRunZeroTeamsFallsBack verifies that when no teams exist, Run executes
// exactly one pass with teamID "" (the dev fallback) and produces one run row.
func TestRunZeroTeamsFallsBack(t *testing.T) {
	store := &multiteamStore{
		// no teams seeded
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			},
			embeddings: map[string][]float32{
				"a": {1, 0, 0, 0},
			},
		},
	}

	llmMock := &mockLLM{response: `{"gaps":[]}`}
	src := &mockAISource{analysisClient: llmMock, improvementClient: llmMock}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Exactly one run row with teamID "".
	if len(store.runs) != 1 {
		t.Fatalf("want exactly 1 run row, got %d", len(store.runs))
	}
	if store.runs[0].TeamID != "" {
		t.Errorf("fallback run should have teamID \"\", got %q", store.runs[0].TeamID)
	}
}

// TestIntervalGatePerTeam verifies that:
// - On an interval trigger, a team below MinEntries is skipped (no run row).
// - On a manual trigger, all teams run regardless of entry count.
func TestIntervalGatePerTeam(t *testing.T) {
	store := &multiteamStore{
		teams: []storage.Team{
			{ID: "small", Enabled: true}, // only 1 entry, below MinEntries=2
			{ID: "big", Enabled: true},   // 2 entries, meets MinEntries
		},
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "s1", Title: "S1", Content: "small", Domain: "x", TeamID: "small"},
				{ID: "b1", Title: "B1", Content: "big1", Domain: "y", TeamID: "big"},
				{ID: "b2", Title: "B2", Content: "big2", Domain: "y", TeamID: "big"},
			},
			embeddings: map[string][]float32{
				"s1": {1, 0, 0, 0},
				"b1": {0, 1, 0, 0},
				"b2": {0, 0.99, 0.14, 0},
			},
		},
	}

	llmMock := &mockLLM{response: `{"title":"Cluster","summary":"Cluster.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	src := &mockAISource{analysisClient: llmMock, improvementClient: llmMock}

	p := New(store, src, Config{
		MinEntries:       2, // "small" has 1 entry → below threshold
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	// Interval trigger: "small" should be skipped.
	if err := p.Run(context.Background(), "interval"); err != nil {
		t.Fatalf("interval Run error: %v", err)
	}
	if len(store.runsForTeam("small")) != 0 {
		t.Errorf("interval trigger: expected 0 runs for 'small' (below MinEntries), got %d", len(store.runsForTeam("small")))
	}
	if len(store.runsForTeam("big")) != 1 {
		t.Errorf("interval trigger: expected 1 run for 'big', got %d", len(store.runsForTeam("big")))
	}

	// Reset run tracking.
	store.mu.Lock()
	store.runs = nil
	store.runsByTeam = nil
	store.snapshots = nil
	store.clusters = nil
	store.mu.Unlock()

	// Manual trigger: ALL teams run regardless.
	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("manual Run error: %v", err)
	}
	if len(store.runsForTeam("small")) != 1 {
		t.Errorf("manual trigger: expected 1 run for 'small', got %d", len(store.runsForTeam("small")))
	}
	if len(store.runsForTeam("big")) != 1 {
		t.Errorf("manual trigger: expected 1 run for 'big', got %d", len(store.runsForTeam("big")))
	}
}

// multiteamAgentStore combines multiteamStore with mockAgentStore so the agent
// generation path can be exercised in multi-team tests.
type multiteamAgentStore struct {
	multiteamStore
	mockAgentStore
}

// TestRunTeamsShareDomain: two teams whose entries cluster into the SAME domain
// name must each produce their own distinct agent row after Run. Neither team's
// agent should overwrite the other's.
func TestRunTeamsShareDomain(t *testing.T) {
	store := &multiteamAgentStore{
		multiteamStore: multiteamStore{
			teams: []storage.Team{
				{ID: "tA", Enabled: true},
				{ID: "tB", Enabled: true},
			},
			mockAnalysisStore: mockAnalysisStore{
				entries: []storage.KnowledgeEntry{
					{ID: "a1", Title: "Finance A1", Content: "Finance pattern A", Domain: "finance", TeamID: "tA"},
					{ID: "a2", Title: "Finance A2", Content: "Finance workflow A", Domain: "finance", TeamID: "tA"},
					{ID: "b1", Title: "Finance B1", Content: "Finance pattern B", Domain: "finance", TeamID: "tB"},
					{ID: "b2", Title: "Finance B2", Content: "Finance workflow B", Domain: "finance", TeamID: "tB"},
				},
				embeddings: map[string][]float32{
					"a1": {1, 0, 0, 0},
					"a2": {0.99, 0.14, 0, 0},
					"b1": {1, 0, 0, 0},
					"b2": {0.99, 0.14, 0, 0},
				},
			},
		},
	}

	agentLLMMock := &mockLLM{
		response: `{"system_prompt":"Finance agent","instructions":"Use DCF.","anti_patterns":"No guessing."}`,
	}
	clusterLLMMock := &mockLLM{
		response: `{"title":"Finance","summary":"Finance cluster.","coherence":0.8,"specificity":0.7,"gaps":[]}`,
	}
	src := newSrcWithAgent(clusterLLMMock, agentLLMMock, clusterLLMMock)

	p := New(&store.multiteamStore, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).WithAgentGeneration(&store.mockAgentStore)

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Both teams should have produced a run.
	if len(store.multiteamStore.runsForTeam("tA")) == 0 {
		t.Error("expected run for tA, got none")
	}
	if len(store.multiteamStore.runsForTeam("tB")) == 0 {
		t.Error("expected run for tB, got none")
	}

	// There must be TWO distinct agents — one per team — even though domain is the same.
	agents := store.mockAgentStore.agents
	if len(agents) < 2 {
		t.Fatalf("expected at least 2 agents (one per team), got %d: %+v", len(agents), agents)
	}

	teamIDs := make(map[string]struct{})
	ids := make(map[string]struct{})
	for _, a := range agents {
		if a.Domain != "finance" {
			continue
		}
		teamIDs[a.TeamID] = struct{}{}
		ids[a.ID] = struct{}{}
	}
	if _, ok := teamIDs["tA"]; !ok {
		t.Error("no finance agent for tA")
	}
	if _, ok := teamIDs["tB"]; !ok {
		t.Error("no finance agent for tB")
	}
	if len(ids) < 2 {
		t.Errorf("expected 2 distinct agent IDs for 'finance', got %d (overwrite detected)", len(ids))
	}
}

// TestRunDisabledTeamSkipped verifies that a disabled team produces no run row
// when Run is called, while an enabled team runs normally.
func TestRunDisabledTeamSkipped(t *testing.T) {
	store := &multiteamStore{
		teams: []storage.Team{
			{ID: "enabled-team", Enabled: true},
			{ID: "disabled-team", Enabled: false},
		},
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "e1", Title: "E1", Content: "enabled content", Domain: "x", TeamID: "enabled-team"},
				{ID: "e2", Title: "E2", Content: "enabled content 2", Domain: "x", TeamID: "enabled-team"},
				{ID: "d1", Title: "D1", Content: "disabled content", Domain: "y", TeamID: "disabled-team"},
			},
			embeddings: map[string][]float32{
				"e1": {1, 0, 0, 0},
				"e2": {0.99, 0.14, 0, 0},
				"d1": {0, 1, 0, 0},
			},
		},
	}

	llmMock := &mockLLM{response: `{"title":"Cluster","summary":"Cluster.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	src := &mockAISource{analysisClient: llmMock, improvementClient: llmMock}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "manual"); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Disabled team must produce zero run rows.
	if len(store.runsForTeam("disabled-team")) != 0 {
		t.Errorf("disabled team should have 0 runs, got %d", len(store.runsForTeam("disabled-team")))
	}

	// Enabled team must have exactly 1 run row.
	if len(store.runsForTeam("enabled-team")) != 1 {
		t.Errorf("enabled team should have 1 run, got %d", len(store.runsForTeam("enabled-team")))
	}
}
