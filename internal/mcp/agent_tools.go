package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func HandleAgentList(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		teamID, _ := resolveActorTeam(ctx)
		agents, err := store.ListAgents(ctx, teamID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list agents: %v", err)), nil
		}
		if agents == nil {
			agents = []storage.Agent{}
		}
		data, _ := json.Marshal(agents)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleAgentGet(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		domain := req.GetString("domain", "")
		if id == "" && domain == "" {
			return mcplib.NewToolResultError("provide either id or domain"), nil
		}

		var a *storage.Agent
		var err error
		if id != "" {
			a, err = store.GetAgent(ctx, id)
		} else {
			teamID, _ := resolveActorTeam(ctx)
			a, err = store.GetAgentByDomain(ctx, domain, teamID)
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}
		tc := auth.GetTeamContext(ctx)
		if !auth.CanAccess(tc, a.TeamID) {
			return mcplib.NewToolResultError("forbidden: agent belongs to another team"), nil
		}

		data, _ := json.Marshal(a)
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func HandleAgentPublish(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		// Fetch before mutating so we can enforce team ownership.
		a, err := store.GetAgent(ctx, id)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}
		tc := auth.GetTeamContext(ctx)
		if !auth.CanAccess(tc, a.TeamID) {
			return mcplib.NewToolResultError("forbidden: agent belongs to another team"), nil
		}
		if err := store.PublishAgent(ctx, id); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("publish agent: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("agent %s published", id)), nil
	}
}

func HandleAgentExport(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		domain := req.GetString("domain", "")
		format := req.GetString("format", "md")

		if id == "" && domain == "" {
			return mcplib.NewToolResultError("provide either id or domain"), nil
		}

		var a *storage.Agent
		var err error
		if id != "" {
			a, err = store.GetAgent(ctx, id)
		} else {
			teamID, _ := resolveActorTeam(ctx)
			a, err = store.GetAgentByDomain(ctx, domain, teamID)
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}
		tc := auth.GetTeamContext(ctx)
		if !auth.CanAccess(tc, a.TeamID) {
			return mcplib.NewToolResultError("forbidden: agent belongs to another team"), nil
		}

		return mcplib.NewToolResultText(agent.Export(a, format)), nil
	}
}
