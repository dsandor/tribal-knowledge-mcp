package storage

import (
	"context"
	"testing"
)

// newAnalysisStore opens a temp SQLite DB with embeddingDim=4 for analysis tests.
func newAnalysisStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := t.TempDir() + "/analysis-test.db"
	store, err := NewSQLiteStore(path, 4)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestAnalysisStore is an alias for newAnalysisStore, used by rules_test.go.
func newTestAnalysisStore(t *testing.T) *SQLiteStore {
	return newAnalysisStore(t)
}

func analysisSampleEntry() KnowledgeEntry {
	return KnowledgeEntry{
		Type:    KTPrompt,
		Title:   "Test Entry",
		Content: "Test content",
		Domain:  "test",
		Tags:    []string{"test"},
		Author:  "tester",
		Team:    "test-team",
	}
}

func TestCountEntries(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	count, err := store.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries: %v", err)
	}
	if count != 0 {
		t.Fatalf("want 0, got %d", count)
	}

	_, err = store.StoreEntry(ctx, analysisSampleEntry(), []float32{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	count, err = store.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries after insert: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1, got %d", count)
	}
}

func TestGetAllEmbeddings(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}
	id, err := store.StoreEntry(ctx, analysisSampleEntry(), emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	embeddings, err := store.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings: %v", err)
	}

	got, ok := embeddings[id]
	if !ok {
		t.Fatalf("embedding for id %q not found", id)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 dimensions, got %d", len(got))
	}
	for i, want := range emb {
		if got[i] != want {
			t.Errorf("embedding[%d]: got %f, want %f", i, got[i], want)
		}
	}
}

func TestGetAllEmbeddings_DeletedEntryAbsent(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	emb := []float32{0.1, 0.2, 0.3, 0.4}
	id, err := store.StoreEntry(ctx, analysisSampleEntry(), emb)
	if err != nil {
		t.Fatalf("StoreEntry: %v", err)
	}

	if err := store.DeleteEntry(ctx, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	embeddings, err := store.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings: %v", err)
	}
	if len(embeddings) != 0 {
		t.Fatalf("want 0 embeddings after delete, got %d", len(embeddings))
	}
}

func TestStoreAndListClusters(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	c := Cluster{
		Domain:        "finance",
		Title:         "Earnings Cluster",
		Summary:       "Cluster of earnings-related prompts",
		EntryIDs:      []string{"id1", "id2", "id3"},
		QualityScore:  0.85,
		PipelineRunID: "run-abc",
	}

	id, err := store.StoreCluster(ctx, c)
	if err != nil {
		t.Fatalf("StoreCluster: %v", err)
	}
	if id == "" {
		t.Fatal("StoreCluster returned empty ID")
	}

	clusters, err := store.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(clusters))
	}
	if clusters[0].Title != "Earnings Cluster" {
		t.Errorf("Title: got %q, want %q", clusters[0].Title, "Earnings Cluster")
	}
	if len(clusters[0].EntryIDs) != 3 {
		t.Errorf("EntryIDs: want 3, got %d", len(clusters[0].EntryIDs))
	}
	wantIDs := map[string]bool{"id1": true, "id2": true, "id3": true}
	for _, id := range clusters[0].EntryIDs {
		if !wantIDs[id] {
			t.Errorf("unexpected entry ID %q in cluster", id)
		}
		delete(wantIDs, id)
	}
	if len(wantIDs) > 0 {
		t.Errorf("missing entry IDs from cluster: %v", wantIDs)
	}
}

func TestPipelineRun(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	runID, err := store.StartPipelineRun(ctx, "manual")
	if err != nil {
		t.Fatalf("StartPipelineRun: %v", err)
	}
	if runID == "" {
		t.Fatal("StartPipelineRun returned empty ID")
	}

	run, err := store.GetLatestPipelineRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestPipelineRun: %v", err)
	}
	if run == nil {
		t.Fatal("GetLatestPipelineRun returned nil")
	}
	if run.Status != "running" {
		t.Errorf("Status: got %q, want %q", run.Status, "running")
	}

	if err := store.FinishPipelineRun(ctx, runID, "complete", 10, 3, nil); err != nil {
		t.Fatalf("FinishPipelineRun: %v", err)
	}

	run, err = store.GetLatestPipelineRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestPipelineRun after finish: %v", err)
	}
	if run == nil {
		t.Fatal("GetLatestPipelineRun returned nil after finish")
	}
	if run.Status != "complete" {
		t.Errorf("Status: got %q, want %q", run.Status, "complete")
	}
	if run.EntriesProcessed != 10 {
		t.Errorf("EntriesProcessed: got %d, want 10", run.EntriesProcessed)
	}
	if run.ClustersFound != 3 {
		t.Errorf("ClustersFound: got %d, want 3", run.ClustersFound)
	}
	if run.CompletedAt == nil {
		t.Error("CompletedAt should be set after FinishPipelineRun")
	}
}

func TestStoreAndGetSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	snap := DatasetSnapshot{
		Version:       1,
		ClusterCount:  5,
		EntryCount:    42,
		Data:          `{"clusters": []}`,
		PipelineRunID: "run-xyz",
	}

	id, err := store.StoreSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("StoreSnapshot: %v", err)
	}
	if id == "" {
		t.Fatal("StoreSnapshot returned empty ID")
	}

	got, err := store.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot: %v", err)
	}
	if got == nil {
		t.Fatal("GetLatestSnapshot returned nil")
	}
	if got.Version != 1 {
		t.Errorf("Version: got %d, want 1", got.Version)
	}
	if got.ClusterCount != 5 {
		t.Errorf("ClusterCount: got %d, want 5", got.ClusterCount)
	}
}

func TestGetLatestPipelineRun_Empty(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	run, err := store.GetLatestPipelineRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestPipelineRun on empty DB: %v", err)
	}
	if run != nil {
		t.Fatalf("want nil, got %+v", run)
	}
}

func TestGetLatestSnapshot_Empty(t *testing.T) {
	ctx := context.Background()
	store := newAnalysisStore(t)

	snap, err := store.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot on empty DB: %v", err)
	}
	if snap != nil {
		t.Fatalf("want nil, got %+v", snap)
	}
}

func TestListSnapshots(t *testing.T) {
	s := newTestAnalysisStore(t)
	ctx := context.Background()

	for _, v := range []int{1, 2, 3} {
		_, err := s.StoreSnapshot(ctx, DatasetSnapshot{
			Version:      v,
			ClusterCount: v * 2,
			EntryCount:   v * 5,
		})
		if err != nil {
			t.Fatalf("StoreSnapshot v%d: %v", v, err)
		}
	}

	snaps, err := s.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("want 3 snapshots, got %d", len(snaps))
	}
	if snaps[0].Version < snaps[1].Version {
		t.Errorf("expected descending version order, got %d then %d", snaps[0].Version, snaps[1].Version)
	}
}
