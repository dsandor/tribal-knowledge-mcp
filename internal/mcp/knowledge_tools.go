package mcp

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterKnowledgeExtTools registers knowledge_search and knowledge_rate.
// bus may be nil; when non-nil a TypeKnowledgeRated live event is published on
// each successful rating.
func RegisterKnowledgeExtTools(s *server.MCPServer, store storage.Store, src *aiconfig.Sources, bus live.EventBus) {
	s.AddTool(
		mcplib.NewTool("knowledge_search",
			mcplib.WithDescription("Call before drafting whenever a task resembles past work, to retrieve relevant patterns, rules, and example outputs by semantic similarity. Use natural-language queries. Always note the entry IDs returned so you can call knowledge_use and knowledge_rate after applying them."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Natural language query")),
			mcplib.WithString("domain", mcplib.Description("Optional domain filter")),
			mcplib.WithNumber("top_k", mcplib.Description("Number of results (default 5, max 20)")),
		),
		logTool("knowledge_search", HandleKnowledgeSearch(store, src)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_rate",
			mcplib.WithDescription("Call after relying on a knowledge entry — rate its helpfulness 1 (not useful) to 5 (excellent). This feedback ranks future retrieval. Treat rating as part of finishing the task, not optional."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID")),
			mcplib.WithNumber("rating", mcplib.Required(), mcplib.Description("1.0 to 5.0")),
		),
		logTool("knowledge_rate", HandleKnowledgeRate(store, bus)),
	)
}

func HandleKnowledgeSearch(store storage.Store, src *aiconfig.Sources) server.ToolHandlerFunc {
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

		// Resolve embedder per call so saved team settings take effect immediately.
		// DefaultTeam fallback is applied inside src.Embedder when teamID is empty.
		teamID, _ := resolveActorTeam(ctx)
		embedder := src.Embedder(ctx, teamID)
		if embedder == nil {
			return mcplib.NewToolResultError("embedding not configured — set OLLAMA_URL to enable knowledge search"), nil
		}

		vec, err := embedder.Embed(ctx, query)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("embed query: %v", err)), nil
		}
		// Over-fetch so the post-search domain/team filters can still fill
		// topK slots when other teams' entries crowd the similarity space.
		fetchK := topK * 2
		if fetchK > 40 {
			fetchK = 40
		}
		results, err := store.SearchSimilar(ctx, vec, fetchK)
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

		// Team-scoping: filter out entries the caller cannot access.
		tc := auth.GetTeamContext(ctx)
		{
			filtered := results[:0]
			for _, r := range results {
				if auth.CanAccess(tc, r.Entry.TeamID) {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		if len(results) > topK {
			results = results[:topK]
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

// HandleKnowledgeRate returns a handler that rates a knowledge entry.
// bus may be nil; when non-nil a TypeKnowledgeRated live event is published on success.
func HandleKnowledgeRate(store storage.Store, bus live.EventBus) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		id := req.GetString("id", "")
		if id == "" {
			return mcplib.NewToolResultError("id is required"), nil
		}
		rating := req.GetFloat("rating", 0)
		if rating < 1 || rating > 5 {
			return mcplib.NewToolResultError("rating must be 1–5"), nil
		}

		if _, errResult := fetchEntryForCaller(ctx, store, id); errResult != nil {
			return errResult, nil
		}

		if err := store.RateEntry(ctx, id, rating); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("rate entry: %v", err)), nil
		}

		// Publish live event (best-effort, nil-safe). This context read is for
		// event attribution only — access gating happened in fetchEntryForCaller.
		teamID, actor := resolveActorTeam(ctx)
		publishEvent(bus, live.LiveEvent{
			Type:    live.TypeKnowledgeRated,
			TeamID:  teamID,
			Actor:   actor,
			EntryID: id,
			Meta:    map[string]string{"rating": strconv.FormatFloat(rating, 'f', -1, 64)},
		})

		return mcplib.NewToolResultText(fmt.Sprintf("Entry %s rated %.1f", id, rating)), nil
	}
}
