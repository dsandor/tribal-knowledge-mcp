package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterResources registers all knowledge and agent resources on the MCP server.
// store must satisfy storage.AgentStore (which embeds AnalysisStore and Store).
func RegisterResources(s *server.MCPServer, store storage.AgentStore) {
	// Static resources
	s.AddResource(
		mcplib.NewResource("knowledge://team/top", "Top Knowledge Entries",
			mcplib.WithResourceDescription("Top 10 approved knowledge entries by rating"),
			mcplib.WithMIMEType("application/json"),
		),
		resourceHandler(func(ctx context.Context, _ mcplib.ReadResourceRequest) (string, error) {
			entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "approved"})
			if err != nil {
				return "", fmt.Errorf("list entries: %w", err)
			}
			data, err := json.Marshal(entries)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)

	s.AddResource(
		mcplib.NewResource("knowledge://team/recent", "Recent Knowledge Entries",
			mcplib.WithResourceDescription("10 most recently approved knowledge entries"),
			mcplib.WithMIMEType("application/json"),
		),
		resourceHandler(func(ctx context.Context, _ mcplib.ReadResourceRequest) (string, error) {
			entries, err := store.ListEntries(ctx, storage.ListFilter{Limit: 10, Status: "approved"})
			if err != nil {
				return "", fmt.Errorf("list entries: %w", err)
			}
			data, err := json.Marshal(entries)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)

	s.AddResource(
		mcplib.NewResource("agents://generated", "Published Agents",
			mcplib.WithResourceDescription("All published AI agents generated from knowledge clusters"),
			mcplib.WithMIMEType("application/json"),
		),
		resourceHandler(func(ctx context.Context, _ mcplib.ReadResourceRequest) (string, error) {
			agents, err := store.ListAgents(ctx)
			if err != nil {
				return "", fmt.Errorf("list agents: %w", err)
			}
			var published []storage.Agent
			for _, a := range agents {
				if a.Status == storage.AgentStatusPublished {
					published = append(published, a)
				}
			}
			data, err := json.Marshal(published)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)

	// Template resources — parameterized by domain or cluster id
	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("knowledge://domain/{name}", "Knowledge by Domain",
			mcplib.WithTemplateDescription("Knowledge entries for a specific domain"),
			mcplib.WithTemplateMIMEType("application/json"),
		),
		resourceTemplateHandler(func(ctx context.Context, req mcplib.ReadResourceRequest) (string, error) {
			domain := extractPathParam(req.Params.URI, "knowledge://domain/")
			entries, err := store.ListEntries(ctx, storage.ListFilter{Domain: domain, Status: "approved", Limit: 50})
			if err != nil {
				return "", fmt.Errorf("list entries: %w", err)
			}
			data, err := json.Marshal(entries)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)

	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("knowledge://cluster/{id}", "Knowledge by Cluster",
			mcplib.WithTemplateDescription("Knowledge entries belonging to a specific cluster"),
			mcplib.WithTemplateMIMEType("application/json"),
		),
		resourceTemplateHandler(func(ctx context.Context, req mcplib.ReadResourceRequest) (string, error) {
			clusterID := extractPathParam(req.Params.URI, "knowledge://cluster/")
			clusters, err := store.ListClusters(ctx)
			if err != nil {
				return "", fmt.Errorf("list clusters: %w", err)
			}
			var entryIDs []string
			for _, c := range clusters {
				if c.ID == clusterID {
					entryIDs = c.EntryIDs
					break
				}
			}
			if len(entryIDs) == 0 {
				return "[]", nil
			}
			var result []storage.KnowledgeEntry
			for _, id := range entryIDs {
				e, err := store.GetEntry(ctx, id)
				if err == nil && e != nil {
					result = append(result, *e)
				}
			}
			data, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)

	s.AddResourceTemplate(
		mcplib.NewResourceTemplate("agents://domain/{name}", "Agent by Domain",
			mcplib.WithTemplateDescription("Latest published agent for a domain"),
			mcplib.WithTemplateMIMEType("application/json"),
		),
		resourceTemplateHandler(func(ctx context.Context, req mcplib.ReadResourceRequest) (string, error) {
			domain := extractPathParam(req.Params.URI, "agents://domain/")
			agent, err := store.GetAgentByDomain(ctx, domain)
			if err != nil {
				return "", fmt.Errorf("get agent: %w", err)
			}
			if agent == nil || agent.Status != storage.AgentStatusPublished {
				return "null", nil
			}
			data, err := json.Marshal(agent)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}),
	)
}

// resourceHandler wraps a simple string-returning func into a ResourceHandlerFunc.
func resourceHandler(fn func(context.Context, mcplib.ReadResourceRequest) (string, error)) server.ResourceHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		text, err := fn(ctx, req)
		if err != nil {
			return nil, err
		}
		return []mcplib.ResourceContents{
			mcplib.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: text},
		}, nil
	}
}

// resourceTemplateHandler wraps a simple string-returning func into a ResourceTemplateHandlerFunc.
func resourceTemplateHandler(fn func(context.Context, mcplib.ReadResourceRequest) (string, error)) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
		text, err := fn(ctx, req)
		if err != nil {
			return nil, err
		}
		return []mcplib.ResourceContents{
			mcplib.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: text},
		}, nil
	}
}

// extractPathParam strips a URI prefix and returns the trailing path segment.
func extractPathParam(uri, prefix string) string {
	if len(uri) > len(prefix) {
		return uri[len(prefix):]
	}
	return ""
}
