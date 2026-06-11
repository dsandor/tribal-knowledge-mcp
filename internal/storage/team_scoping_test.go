package storage

import (
	"context"
	"testing"
)

func TestGetAllEmbeddingsTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()

	emb := []float32{0.1, 0.2, 0.3, 0.4}

	id1, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPrompt, Title: "e1", Content: "c1", TeamID: "t1"}, emb)
	if err != nil {
		t.Fatalf("StoreEntry t1: %v", err)
	}
	id2, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPrompt, Title: "e2", Content: "c2", TeamID: "t2"}, emb)
	if err != nil {
		t.Fatalf("StoreEntry t2: %v", err)
	}

	// Scoped to t1: should only contain id1.
	got1, err := s.GetAllEmbeddings(ctx, "t1")
	if err != nil {
		t.Fatalf("GetAllEmbeddings(t1): %v", err)
	}
	if len(got1) != 1 {
		t.Fatalf("GetAllEmbeddings(t1) returned %d entries, want 1", len(got1))
	}
	if _, ok := got1[id1]; !ok {
		t.Errorf("GetAllEmbeddings(t1) missing id1 %q; got keys: %v", id1, mapKeys(got1))
	}
	if _, ok := got1[id2]; ok {
		t.Errorf("GetAllEmbeddings(t1) unexpectedly contains id2 %q", id2)
	}

	// Unscoped: should contain both.
	gotAll, err := s.GetAllEmbeddings(ctx, "")
	if err != nil {
		t.Fatalf("GetAllEmbeddings(\"\"): %v", err)
	}
	if len(gotAll) != 2 {
		t.Fatalf("GetAllEmbeddings(\"\") returned %d entries, want 2", len(gotAll))
	}
	if _, ok := gotAll[id1]; !ok {
		t.Errorf("GetAllEmbeddings(\"\") missing id1 %q", id1)
	}
	if _, ok := gotAll[id2]; !ok {
		t.Errorf("GetAllEmbeddings(\"\") missing id2 %q", id2)
	}
}

func mapKeys(m map[string][]float32) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestClusterTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StoreCluster(ctx, Cluster{Title: "a", TeamID: "t1"}); err != nil {
		t.Fatalf("store cluster t1: %v", err)
	}
	if _, err := s.StoreCluster(ctx, Cluster{Title: "b", TeamID: "t2"}); err != nil {
		t.Fatalf("store cluster t2: %v", err)
	}
	got, err := s.ListClusters(ctx, "t1")
	if err != nil {
		t.Fatalf("list t1: %v", err)
	}
	if len(got) != 1 || got[0].TeamID != "t1" {
		t.Fatalf("ListClusters(t1) = %+v, want 1 cluster with TeamID t1", got)
	}
	all, err := s.ListClusters(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListClusters(\"\") returned %d, want 2", len(all))
	}
}

func TestAgentTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "d1", TeamID: "t1"}); err != nil {
		t.Fatalf("upsert t1: %v", err)
	}
	if _, err := s.UpsertAgent(ctx, Agent{Domain: "d2", TeamID: "t2"}); err != nil {
		t.Fatalf("upsert t2: %v", err)
	}
	got, err := s.ListAgents(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].TeamID != "t1" {
		t.Fatalf("ListAgents(t1) = %+v, want 1 agent with TeamID t1", got)
	}
	all, err := s.ListAgents(ctx, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAgents(\"\") returned %d, want 2", len(all))
	}
}

func TestSnapshotAndRunTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StartPipelineRun(ctx, "manual", "t1"); err != nil {
		t.Fatalf("start run: %v", err)
	}
	if _, err := s.StartPipelineRun(ctx, "manual", "t2"); err != nil {
		t.Fatalf("start run t2: %v", err)
	}
	runs, err := s.ListPipelineRuns(ctx, "t1", 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].TeamID != "t1" {
		t.Fatalf("ListPipelineRuns(t1) = %+v, want 1 run for t1", runs)
	}
	latest, err := s.GetLatestPipelineRun(ctx, "t1")
	if err != nil || latest == nil || latest.TeamID != "t1" {
		t.Fatalf("GetLatestPipelineRun(t1) = %+v err=%v, want t1 run", latest, err)
	}

	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 1, TeamID: "t1"}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 2, TeamID: "t2"}); err != nil {
		t.Fatalf("store snapshot t2: %v", err)
	}
	snaps, err := s.ListSnapshots(ctx, "t1")
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].TeamID != "t1" {
		t.Fatalf("ListSnapshots(t1) = %+v, want 1 snapshot for t1", snaps)
	}
}

func TestCountEntriesTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "a", Content: "c", TeamID: "t1"}, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "b", Content: "c", TeamID: "t2"}, nil); err != nil {
		t.Fatalf("store: %v", err)
	}
	n, err := s.CountEntries(ctx, "t1")
	if err != nil || n != 1 {
		t.Fatalf("CountEntries(t1) = %d err=%v, want 1", n, err)
	}
	all, err := s.CountEntries(ctx, "")
	if err != nil || all != 2 {
		t.Fatalf("CountEntries(\"\") = %d err=%v, want 2", all, err)
	}
}

func TestGetLatestSnapshotTeamScoping(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 1, TeamID: "t1"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 2, TeamID: "t2"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, err := s.GetLatestSnapshot(ctx, "t1")
	if err != nil || got == nil || got.TeamID != "t1" || got.Version != 1 {
		t.Fatalf("GetLatestSnapshot(t1) = %+v err=%v, want t1 v1", got, err)
	}
	global, err := s.GetLatestSnapshot(ctx, "")
	if err != nil || global == nil || global.Version != 2 {
		t.Fatalf("GetLatestSnapshot(\"\") = %+v err=%v, want v2", global, err)
	}
}

func TestBackfillTeamID(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	idEmpty, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "legacy", Content: "c"}, nil)
	if err != nil {
		t.Fatalf("store legacy: %v", err)
	}
	idOwned, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "owned", Content: "c2", TeamID: "t9"}, nil)
	if err != nil {
		t.Fatalf("store owned: %v", err)
	}
	if _, err := s.StoreCluster(ctx, Cluster{Title: "legacy-cluster"}); err != nil {
		t.Fatalf("store cluster: %v", err)
	}
	if _, err := s.StoreSnapshot(ctx, DatasetSnapshot{Version: 7}); err != nil {
		t.Fatalf("store snapshot: %v", err)
	}
	if _, err := s.StartPipelineRun(ctx, "manual", ""); err != nil {
		t.Fatalf("start run: %v", err)
	}

	if err := s.BackfillTeamID(ctx, "t1"); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	e, err := s.GetEntry(ctx, idEmpty)
	if err != nil {
		t.Fatalf("get legacy: %v", err)
	}
	if e.TeamID != "t1" {
		t.Fatalf("legacy entry team = %q, want t1", e.TeamID)
	}
	o, err := s.GetEntry(ctx, idOwned)
	if err != nil {
		t.Fatalf("get owned: %v", err)
	}
	if o.TeamID != "t9" {
		t.Fatalf("owned entry team = %q, want t9 (untouched)", o.TeamID)
	}
	clusters, err := s.ListClusters(ctx, "t1")
	if err != nil {
		t.Fatalf("list clusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("cluster not backfilled: %+v", clusters)
	}
	snaps, err := s.ListSnapshots(ctx, "t1")
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshot not backfilled: %+v err=%v", snaps, err)
	}
	runs, err := s.ListPipelineRuns(ctx, "t1", 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("pipeline run not backfilled: %+v err=%v", runs, err)
	}

	// Idempotent: second run succeeds and changes nothing.
	if err := s.BackfillTeamID(ctx, "t1"); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	// Empty teamID is a no-op, not an error.
	if err := s.BackfillTeamID(ctx, ""); err != nil {
		t.Fatalf("empty backfill: %v", err)
	}
}

// TestStoreEntryStatusDefaultSQLite pins the SQLite default: entries stored
// with no Status (e.g. via stdio MCP) must land as "approved". Postgres
// intentionally defaults to "pending"; changing this default would hide
// MCP-stored entries from status-filtered UI lists.
func TestStoreEntryStatusDefaultSQLite(t *testing.T) {
	s := newTestStoreInternal(t)
	ctx := context.Background()
	id, err := s.StoreEntry(ctx, KnowledgeEntry{Type: KTPattern, Title: "t", Content: "c"}, nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	e, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if e.Status != "approved" {
		t.Fatalf("default status = %q, want approved", e.Status)
	}
}
