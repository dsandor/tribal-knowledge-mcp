package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

func TestPipeline_Run_CreatesClustersAndSnapshot(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	// mockLLM responses: summarize returns JSON, score/detect return valid JSON too
	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}

	p := New(store, newSrc(llmMock), Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(store.clusters) != 1 {
		t.Errorf("want 1 cluster, got %d", len(store.clusters))
	}
	if len(store.snapshots) != 1 {
		t.Errorf("want 1 snapshot, got %d", len(store.snapshots))
	}
	if len(store.runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(store.runs))
	}
	if store.runs[0].Status == "running" {
		t.Error("run should be completed, not running")
	}
	if store.snapshots[0].EntryCount != 2 {
		t.Errorf("snapshot entry_count = %d, want 2", store.snapshots[0].EntryCount)
	}
	if store.snapshots[0].ClusterCount != 1 {
		t.Errorf("snapshot cluster_count = %d, want 1", store.snapshots[0].ClusterCount)
	}
	if store.snapshots[0].Version != 1 {
		t.Errorf("snapshot version = %d, want 1", store.snapshots[0].Version)
	}
}

// TestPipeline_Run_SkipsWhenAnalysisLLMNil verifies that when AnalysisLLM returns
// nil (no Anthropic key configured), Run returns nil error and does not write any
// pipeline-run records, clusters, or snapshots to the store.
func TestPipeline_Run_SkipsWhenAnalysisLLMNil(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
		},
	}

	// mockAISource with a nil analysisClient simulates "no Anthropic key configured".
	src := &mockAISource{analysisClient: nil}

	p := New(store, src, Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	err := p.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected nil error when AnalysisLLM is nil, got: %v", err)
	}
	if len(store.runs) != 0 {
		t.Errorf("expected 0 pipeline run records written, got %d", len(store.runs))
	}
	if len(store.clusters) != 0 {
		t.Errorf("expected 0 clusters written, got %d", len(store.clusters))
	}
	if len(store.snapshots) != 0 {
		t.Errorf("expected 0 snapshots written, got %d", len(store.snapshots))
	}
}

func TestPipeline_Run_NoClusters_DissimilarEntries(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "A", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "B", Domain: "legal"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0, 0, 0, 1},
		},
	}
	llmMock := &mockLLM{response: `{"gaps":[]}`}

	p := New(store, newSrc(llmMock), Config{
		MinEntries:       1,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	})

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(store.clusters) != 0 {
		t.Errorf("want 0 clusters for dissimilar entries, got %d", len(store.clusters))
	}
	if len(store.snapshots) != 1 {
		t.Errorf("snapshot should still be created, got %d", len(store.snapshots))
	}
}

func TestPipeline_Run_VersionIncrement(t *testing.T) {
	store := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "A", Content: "A", Domain: "x"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0},
		},
		snapshots: []storage.DatasetSnapshot{
			{ID: "old", Version: 3},
		},
	}
	llmMock := &mockLLM{response: `{"gaps":[]}`}

	p := New(store, newSrc(llmMock), Config{MinEntries: 1, Interval: time.Hour, ClusterThreshold: 0.9})
	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	// GetLatestSnapshot returns v3, so new snapshot should be v4
	if store.snapshots[len(store.snapshots)-1].Version != 4 {
		t.Errorf("expected version 4, got %d", store.snapshots[len(store.snapshots)-1].Version)
	}
}

func TestPipeline_Run_GeneratesAgentWhenStoreProvided(t *testing.T) {
	baseStore := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	agentStore := &mockAgentStore{}

	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	agentLLMMock := &mockLLM{response: `{"system_prompt":"You are a finance agent.","instructions":"Use DCF.","anti_patterns":"No guessing."}`}

	p := New(baseStore, newSrcWithAgent(llmMock, agentLLMMock, llmMock), Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).WithAgentGeneration(agentStore)

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	if len(agentStore.agents) != 1 {
		t.Errorf("want 1 agent generated, got %d", len(agentStore.agents))
	}
	if agentStore.agents[0].Domain != "finance" {
		t.Errorf("agent domain = %q, want finance", agentStore.agents[0].Domain)
	}
	if agentStore.agents[0].SystemPrompt == "" {
		t.Error("agent system_prompt should not be empty")
	}
	if agentStore.agents[0].Version != 1 {
		t.Errorf("agent version = %d, want 1 for initial generation", agentStore.agents[0].Version)
	}
	if len(agentStore.agentVersions) != 1 {
		t.Errorf("want 1 agent version stored, got %d", len(agentStore.agentVersions))
	}
	if agentStore.agentVersions[0].Changelog != "initial generation" {
		t.Errorf("changelog = %q, want initial generation", agentStore.agentVersions[0].Changelog)
	}
}

func TestPipeline_Run_IncrementsAgentVersion(t *testing.T) {
	baseStore := &mockAnalysisStore{
		entries: []storage.KnowledgeEntry{
			{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance"},
			{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance"},
		},
		embeddings: map[string][]float32{
			"a": {1, 0, 0, 0},
			"b": {0.99, 0.14, 0, 0},
		},
	}
	agentStore := &mockAgentStore{}

	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	agentLLMMock := &mockLLM{response: `{"system_prompt":"You are a finance agent.","instructions":"Use DCF.","anti_patterns":"No guessing."}`}
	src := newSrcWithAgent(llmMock, agentLLMMock, llmMock)

	p := New(baseStore, src, Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).WithAgentGeneration(agentStore)

	// First run creates the agent at version 1.
	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("first pipeline run: %v", err)
	}
	if len(agentStore.agents) != 1 || agentStore.agents[0].Version != 1 {
		t.Fatalf("after first run: want 1 agent at version 1, got %d agents", len(agentStore.agents))
	}

	// Second run updates the agent: version should increment to 2.
	agentLLMMock.response = `{"system_prompt":"Updated finance agent.","instructions":"Use DCF and NPV.","anti_patterns":"No guessing."}`
	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("second pipeline run: %v", err)
	}

	if agentStore.agents[0].Version != 2 {
		t.Errorf("after second run: agent version = %d, want 2", agentStore.agents[0].Version)
	}
	if len(agentStore.agentVersions) != 2 {
		t.Errorf("want 2 agent versions stored, got %d", len(agentStore.agentVersions))
	}
	if agentStore.agentVersions[1].Changelog == "initial generation" {
		t.Error("second run changelog should not be 'initial generation'")
	}
}
