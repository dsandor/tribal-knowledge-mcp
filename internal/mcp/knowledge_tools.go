package mcp

import (
	"context"
	"fmt"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterKnowledgeExtTools(s *server.MCPServer, store storage.Store, embedder embedding.Embedder) {
	s.AddTool(
		mcplib.NewTool("knowledge_search",
			mcplib.WithDescription("Call before drafting whenever a task resembles past work, to retrieve relevant patterns, rules, and example outputs by semantic similarity. Use natural-language queries. Always note the entry IDs returned so you can call knowledge_use and knowledge_rate after applying them."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Natural language query")),
			mcplib.WithString("domain", mcplib.Description("Optional domain filter")),
			mcplib.WithNumber("top_k", mcplib.Description("Number of results (default 5, max 20)")),
		),
		HandleKnowledgeSearch(store, embedder),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_rate",
			mcplib.WithDescription("Call after relying on a knowledge entry — rate its helpfulness 1 (not useful) to 5 (excellent). This feedback ranks future retrieval. Treat rating as part of finishing the task, not optional."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID")),
			mcplib.WithNumber("rating", mcplib.Required(), mcplib.Description("1.0 to 5.0")),
		),
		HandleKnowledgeRate(store),
	)
}

func HandleKnowledgeSearch(store storage.Store, embedder embedding.Embedder) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		query := req.GetString("query", "")
		if query == "" {
			return mcplib.NewToolResultError("query is required"), nil
		}
		topKF := req.GetFloat("top_k", 5)
		topK := int(topKF)
		if topK <= 0 {
			topK = 5
		}
		if topK > 20 {
			topK = 20
		}

		vec, err := embedder.Embed(ctx, query)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("embed query: %v", err)), nil
		}
		results, err := store.SearchSimilar(ctx, vec, topK)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("search: %v", err)), nil
		}

		domain := req.GetString("domain", "")
		if domain != "" {
			filtered := results[:0]
			for _, r := range results {
				if r.Entry.Domain == domain {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		if len(results) == 0 {
			return mcplib.NewToolResultText("No similar entries found."), nil
		}
		out := ""
		for i, r := range results {
			out += fmt.Sprintf("[%d] id=%s (score=%.3f) %s\n  Domain: %s\n  %s\n\n",
				i+1, r.Entry.ID, r.Score, r.Entry.Title, r.Entry.Domain, r.Entry.Content)
		}
		return mcplib.NewToolResultText(out), nil
	}
}

func HandleKnowledgeRate(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		rating := req.GetFloat("rating", 0)
		if rating < 1 || rating > 5 {
			return mcplib.NewToolResultError("rating must be 1–5"), nil
		}
		if err := store.RateEntry(ctx, id, rating); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("rate entry: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Entry %s rated %.1f", id, rating)), nil
	}
}
