package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterPromptSuggest(s *server.MCPServer, store storage.Store, src *aiconfig.Sources) {
	s.AddTool(
		mcplib.NewTool("prompt_suggest",
			mcplib.WithDescription("Call to get an LLM-improved version of a draft prompt, using high-rated team knowledge as examples. Use when you want a rewritten prompt, not just context. Prefer enrich_context when you need both rules and knowledge in one call."),
			mcplib.WithString("prompt", mcplib.Required(), mcplib.Description("Draft prompt to improve")),
			mcplib.WithString("domain", mcplib.Description("Optional domain to focus suggestions")),
		),
		logTool("prompt_suggest", HandlePromptSuggest(store, src)),
	)

	s.AddPrompt(
		mcplib.NewPrompt("enhance_with_context",
			mcplib.WithPromptDescription("Wrap a prompt with team knowledge context"),
			mcplib.WithArgument("prompt", mcplib.ArgumentDescription("The prompt to enhance"), mcplib.RequiredArgument()),
			mcplib.WithArgument("domain", mcplib.ArgumentDescription("Domain for rule and agent lookup (optional)")),
			mcplib.WithArgument("team", mcplib.ArgumentDescription("Team identifier (optional)")),
		),
		HandleEnhanceWithContext(store, src),
	)
}

func HandlePromptSuggest(store storage.Store, src *aiconfig.Sources) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		prompt := req.GetString("prompt", "")
		if prompt == "" {
			return mcplib.NewToolResultError("prompt is required"), nil
		}
		domain := req.GetString("domain", "")

		// Resolve effective team and AI clients per call.
		teamID, _ := resolveActorTeam(ctx)
		if teamID == "" {
			teamID = src.DefaultTeam
		}
		embedder := src.Embedder(ctx, teamID)
		llmClient := src.EnrichmentLLM(ctx, teamID)

		if embedder == nil || llmClient == nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		vec, err := embedder.Embed(ctx, prompt)
		if err != nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		const wantK = 5
		fetchK := wantK * 2
		if fetchK > 40 {
			fetchK = 40
		}
		results, err := store.SearchSimilar(ctx, vec, fetchK)
		if err != nil {
			return mcplib.NewToolResultText(prompt), nil
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
		if len(results) > wantK {
			results = results[:wantK]
		}
		if len(results) == 0 {
			return mcplib.NewToolResultText(prompt), nil
		}

		var topEntries []storage.SearchResult
		for _, r := range results {
			if r.Entry.Rating >= 3.5 && (domain == "" || r.Entry.Domain == domain) {
				topEntries = append(topEntries, r)
				if len(topEntries) >= 3 {
					break
				}
			}
		}
		limit := 3
		if len(results) < limit {
			limit = len(results)
		}
		if len(topEntries) == 0 {
			topEntries = results[:limit]
		}

		examplesBlock := strings.Builder{}
		for i, e := range topEntries {
			examplesBlock.WriteString(fmt.Sprintf("Example %d (rating=%.1f):\n%s\n\n", i+1, e.Entry.Rating, e.Entry.Content))
		}

		// llm.Client.Complete takes ONE string — combine system+user into one prompt
		combinedPrompt := fmt.Sprintf(
			"You are a prompt engineering expert. Given a draft prompt and examples of high-quality prompts from the team, suggest an improved version. Return JSON: {\"improved_prompt\": \"...\", \"rationale\": \"...\"}\n\nDraft prompt:\n%s\n\nHigh-quality team examples:\n%s\nReturn improved JSON.",
			prompt, examplesBlock.String(),
		)

		resp, err := llmClient.Complete(ctx, combinedPrompt)
		if err != nil {
			return mcplib.NewToolResultText(prompt), nil
		}

		sourceIDs := make([]string, 0, len(topEntries))
		for _, e := range topEntries {
			sourceIDs = append(sourceIDs, e.Entry.ID)
		}

		out := fmt.Sprintf("Suggested prompt:\n%s\n\nSource entries used: %s", resp, strings.Join(sourceIDs, ", "))
		return mcplib.NewToolResultText(out), nil
	}
}

func HandleEnhanceWithContext(store storage.Store, src *aiconfig.Sources) func(context.Context, mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
	return func(ctx context.Context, req mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
		prompt := req.Params.Arguments["prompt"]
		domain := req.Params.Arguments["domain"]
		team := req.Params.Arguments["team"]

		// Resolve effective team and embedder per call.
		teamID, _ := resolveActorTeam(ctx)
		if teamID == "" && team != "" {
			teamID = team
		}
		if teamID == "" {
			teamID = src.DefaultTeam
		}
		embedder := src.Embedder(ctx, teamID)

		preamble := strings.Builder{}

		if ruleStore, ok := store.(interface {
			GetApplicableRules(ctx context.Context, team, category, user string) ([]storage.Rule, error)
		}); ok {
			rules, _ := ruleStore.GetApplicableRules(ctx, team, domain, "")
			if len(rules) > 0 {
				preamble.WriteString("## Team Rules\n")
				for _, r := range rules {
					preamble.WriteString(fmt.Sprintf("- %s: %s\n", r.Title, r.Content))
				}
				preamble.WriteString("\n")
			}
		}

		if embedder != nil && prompt != "" {
			vec, err := embedder.Embed(ctx, prompt)
			if err == nil {
				const wantK2 = 3
				fetchK2 := wantK2 * 2
				if fetchK2 > 40 {
					fetchK2 = 40
				}
				results, err := store.SearchSimilar(ctx, vec, fetchK2)
				if err == nil {
					// Team-scoping: filter out entries the caller cannot access.
					tc2 := auth.GetTeamContext(ctx)
					filtered2 := results[:0]
					for _, r := range results {
						if auth.CanAccess(tc2, r.Entry.TeamID) {
							filtered2 = append(filtered2, r)
						}
					}
					results = filtered2
					if len(results) > wantK2 {
						results = results[:wantK2]
					}
					if len(results) > 0 {
						preamble.WriteString("## Relevant Team Knowledge\n")
						for _, r := range results {
							preamble.WriteString(fmt.Sprintf("### %s\n%s\n\n", r.Entry.Title, r.Entry.Content))
						}
					}
				}
			}
		}

		if domain != "" {
			if agentStore, ok := store.(interface {
				GetAgentByDomain(ctx context.Context, domain, teamID string) (*storage.Agent, error)
			}); ok {
				a, _ := agentStore.GetAgentByDomain(ctx, domain, teamID)
				if a != nil && a.Status == storage.AgentStatusPublished {
					preamble.WriteString(fmt.Sprintf("## Domain Agent: %s\n%s\n\n", a.Domain, a.SystemPrompt))
				}
			}
		}

		fullPrompt := prompt
		if preamble.Len() > 0 {
			fullPrompt = preamble.String() + "## Your Request\n" + prompt
		}

		return &mcplib.GetPromptResult{
			Description: "Prompt enhanced with team knowledge context",
			Messages: []mcplib.PromptMessage{
				{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: fullPrompt}},
			},
		}, nil
	}
}
