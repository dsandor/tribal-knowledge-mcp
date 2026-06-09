package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/agent"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func HandleAgentList(store storage.AgentStore) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		agents, err := store.ListAgents(ctx)
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
			a, err = store.GetAgentByDomain(ctx, domain)
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
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
			a, err = store.GetAgentByDomain(ctx, domain)
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("get agent: %v", err)), nil
		}
		if a == nil {
			return mcplib.NewToolResultError("agent not found"), nil
		}

		return mcplib.NewToolResultText(agent.Export(a, format)), nil
	}
}
