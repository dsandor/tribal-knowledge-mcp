package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// mockAnalysisStore extends mockStore with AnalysisStore methods.
// mockStore is defined in tools_test.go (same package mcp_test).
type mockAnalysisStore struct {
	mockStore
	clusters []storage.Cluster
	run      *storage.PipelineRun
	snapshot *storage.DatasetSnapshot
}

func (m *mockAnalysisStore) CountEntries(_ context.Context) (int, error) { return 0, nil }
func (m *mockAnalysisStore) GetAllEmbeddings(_ context.Context) (map[string][]float32, error) {
	return nil, nil
}
func (m *mockAnalysisStore) ListClusters(_ context.Context) ([]storage.Cluster, error) {
	return m.clusters, nil
}
func (m *mockAnalysisStore) StoreCluster(_ context.Context, _ storage.Cluster) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) DeleteClustersByRunID(_ context.Context, _ string) error { return nil }
func (m *mockAnalysisStore) StartPipelineRun(_ context.Context, _ string) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) FinishPipelineRun(_ context.Context, _, _ string, _, _ int, _ []string) error {
	return nil
}
func (m *mockAnalysisStore) GetLatestPipelineRun(_ context.Context) (*storage.PipelineRun, error) {
	return m.run, nil
}
func (m *mockAnalysisStore) StoreSnapshot(_ context.Context, _ storage.DatasetSnapshot) (string, error) {
	return "id", nil
}
func (m *mockAnalysisStore) GetLatestSnapshot(_ context.Context) (*storage.DatasetSnapshot, error) {
	return m.snapshot, nil
}
func (m *mockAnalysisStore) ListSnapshots(_ context.Context) ([]storage.DatasetSnapshot, error) {
	return nil, nil
}
func (m *mockAnalysisStore) RateEntry(_ context.Context, _ string, _ float64) error { return nil }

func TestHandleClusterList_ReturnsClusters(t *testing.T) {
	store := &mockAnalysisStore{
		clusters: []storage.Cluster{
			{ID: "c1", Title: "Finance Cluster", Summary: "Finance entries", Domain: "finance"},
		},
	}
	handler := internalmcp.HandleClusterList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
	var clusters []map[string]any
	if err := json.Unmarshal([]byte(textContent(result)), &clusters); err != nil {
		t.Fatalf("parse clusters JSON: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if clusters[0]["Title"] != "Finance Cluster" {
		t.Errorf("title: got %v, want Finance Cluster", clusters[0]["Title"])
	}
}

func TestHandleClusterList_Empty(t *testing.T) {
	store := &mockAnalysisStore{}
	handler := internalmcp.HandleClusterList(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
}

func TestHandleAnalysisStatus_WithData(t *testing.T) {
	now := time.Now()
	store := &mockAnalysisStore{
		run:      &storage.PipelineRun{ID: "r1", Status: "complete", StartedAt: now},
		snapshot: &storage.DatasetSnapshot{ID: "s1", Version: 2, ClusterCount: 3, EntryCount: 15},
	}
	handler := internalmcp.HandleAnalysisStatus(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error")
	}
	var status map[string]json.RawMessage
	if err := json.Unmarshal([]byte(textContent(result)), &status); err != nil {
		t.Fatalf("parse status JSON: %v", err)
	}
	if string(status["pipeline_run"]) == "null" {
		t.Error("pipeline_run should not be null")
	}
	if string(status["latest_snapshot"]) == "null" {
		t.Error("latest_snapshot should not be null")
	}
}

func TestHandleAnalysisStatus_NoData(t *testing.T) {
	store := &mockAnalysisStore{}
	handler := internalmcp.HandleAnalysisStatus(store)
	result, err := handler(context.Background(), mcplib.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected tool error for empty status")
	}
}
