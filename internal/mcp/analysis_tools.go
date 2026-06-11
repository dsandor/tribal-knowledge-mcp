package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// HandleClusterList returns a handler that lists all knowledge clusters.
func HandleClusterList(store storage.AnalysisStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		teamID, _ := resolveActorTeam(ctx)
		clusters, err := store.ListClusters(ctx, teamID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list clusters: %v", err)), nil
		}
		if clusters == nil {
			clusters = []storage.Cluster{}
		}
		data, err := json.Marshal(clusters)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal clusters: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// HandleAnalysisStatus returns a handler that reports pipeline and snapshot status.
func HandleAnalysisStatus(store storage.AnalysisStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		teamID, _ := resolveActorTeam(ctx)
		run, err := store.GetLatestPipelineRun(ctx, teamID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get pipeline run: %v", err)), nil
		}
		snap, err := store.GetLatestSnapshot(ctx, teamID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get snapshot: %v", err)), nil
		}
		status := map[string]any{
			"pipeline_run":    run,
			"latest_snapshot": snap,
		}
		data, err := json.Marshal(status)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal status: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}
