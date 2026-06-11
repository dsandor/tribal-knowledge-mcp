package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// TestPipelineStampsTeamID verifies that every artifact written during a pipeline
// run (Cluster, DatasetSnapshot, Agent) carries the correct teamID.
//
// UPDATED in Task 4: Previously this test set p.teamID via WithWeakSignalImprovement("t1").
// The p.teamID field no longer exists; teamID now comes from ListTeams. The test
// now seeds a multiteamStore with one team "t1" and entries tagged to that team,
// so the per-team loop produces artifacts stamped with "t1".
func TestPipelineStampsTeamID(t *testing.T) {
	const teamID = "t1"

	store := &multiteamStore{
		teams: []storage.Team{{ID: teamID, Enabled: true}},
		mockAnalysisStore: mockAnalysisStore{
			entries: []storage.KnowledgeEntry{
				{ID: "a", Title: "Entry A", Content: "Finance pattern", Domain: "finance", TeamID: teamID},
				{ID: "b", Title: "Entry B", Content: "Finance workflow", Domain: "finance", TeamID: teamID},
			},
			embeddings: map[string][]float32{
				"a": {1, 0, 0, 0},
				"b": {0.99, 0.14, 0, 0},
			},
		},
	}
	agentStore := &mockAgentStore{}

	llmMock := &mockLLM{response: `{"title":"Finance","summary":"Finance entries.","coherence":0.8,"specificity":0.7,"gaps":[]}`}
	agentLLMMock := &mockLLM{response: `{"system_prompt":"You are a finance agent.","instructions":"Use DCF.","anti_patterns":"No guessing."}`}

	p := New(store, newSrcWithAgent(llmMock, agentLLMMock, llmMock), Config{
		MinEntries:       2,
		Interval:         time.Hour,
		ClusterThreshold: 0.9,
	}).
		WithWeakSignalImprovement().
		WithAgentGeneration(agentStore)

	if err := p.Run(context.Background(), "test"); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	// --- clusters ---
	if len(store.clusters) == 0 {
		t.Fatal("expected at least one cluster to be stored")
	}
	for i, c := range store.clusters {
		if c.TeamID != teamID {
			t.Errorf("cluster[%d].TeamID = %q, want %q", i, c.TeamID, teamID)
		}
	}

	// --- snapshots ---
	if len(store.snapshots) == 0 {
		t.Fatal("expected at least one snapshot to be stored")
	}
	for i, s := range store.snapshots {
		if s.TeamID != teamID {
			t.Errorf("snapshot[%d].TeamID = %q, want %q", i, s.TeamID, teamID)
		}
	}

	// --- agents ---
	if len(agentStore.agents) == 0 {
		t.Fatal("expected at least one agent to be stored")
	}
	for i, a := range agentStore.agents {
		if a.TeamID != teamID {
			t.Errorf("agent[%d].TeamID = %q, want %q", i, a.TeamID, teamID)
		}
	}
}
