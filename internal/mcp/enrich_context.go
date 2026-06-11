package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterEnrichContext registers the enrich_context tool with the MCP server.
// src provides per-call resolved LLM and embedder; when both resolve to nil the
// improved_prompt is the rule-enhanced version only.
// bus may be nil; in that case no live events are published.
func RegisterEnrichContext(s *server.MCPServer, store storage.Store, src *aiconfig.Sources, bus live.EventBus) {
	s.AddTool(
		mcplib.NewTool("enrich_context",
			mcplib.WithDescription("ALWAYS call this FIRST, at the start of every user turn, before planning or drafting a response. Pass the raw user message plus optional team/category/user context. Returns improved_prompt (enhanced with applicable rules), applicable_rules (rule titles and content), relevant_knowledge (top matching entries with IDs), and missing_inputs (checklist of any context that would help). Idempotent and cheap — when in doubt, call it."),
			mcplib.WithString("prompt", mcplib.Required(), mcplib.Description("The raw user message or task description")),
			mcplib.WithString("team", mcplib.Description("Team identifier for scoping rules")),
			mcplib.WithString("category", mcplib.Description("Domain/category for scoping rules and knowledge search")),
			mcplib.WithString("user", mcplib.Description("User identifier for user-scoped rules")),
		),
		HandleEnrichContext(store, src, bus),
	)
}

// enrichContextRule is the JSON-serializable form of a matched rule.
type enrichContextRule struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Scope   string `json:"scope"`
}

// enrichContextKnowledge is the JSON-serializable form of a matched knowledge entry.
type enrichContextKnowledge struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Domain  string  `json:"domain"`
	Content string  `json:"content"`
}

// enrichContextResponse is the top-level response returned by enrich_context.
type enrichContextResponse struct {
	ImprovedPrompt    string                   `json:"improved_prompt"`
	ApplicableRules   []enrichContextRule      `json:"applicable_rules"`
	RelevantKnowledge []enrichContextKnowledge `json:"relevant_knowledge"`
	MissingInputs     []string                 `json:"missing_inputs"`
}

// HandleEnrichContext returns a ToolHandlerFunc that enriches a prompt with applicable rules,
// relevant knowledge entries, and optionally an LLM-improved version of the prompt.
// bus may be nil; when non-nil one TypeEnrichContext live event is published per call
// and one ActivityEvent is persisted to the store (best-effort, never fails the tool call).
func HandleEnrichContext(store storage.Store, src *aiconfig.Sources, bus live.EventBus) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		prompt := req.GetString("prompt", "")
		if prompt == "" {
			return mcplib.NewToolResultError("prompt is required"), nil
		}
		team := req.GetString("team", "")
		category := req.GetString("category", "")
		user := req.GetString("user", "")

		// --- Resolve actor/team for live events ---
		teamID, actor := resolveActorTeam(ctx)
		// If the tool received an explicit "team" or "user" arg and we are in
		// the stdio fallback (no auth context), prefer the tool args.
		if actor.ID == "stdio" {
			if user != "" {
				actor = live.ActorRef{ID: user, Display: user}
			}
			if team != "" {
				teamID = team
			}
		}

		// Resolve effective team for config lookups: prefer auth-resolved teamID,
		// then explicit tool arg, then default.
		effectiveTeam := teamID
		if effectiveTeam == "" && team != "" {
			effectiveTeam = team
		}
		if effectiveTeam == "" {
			effectiveTeam = src.DefaultTeam
		}

		// Resolve AI clients per call so saved settings take effect immediately.
		embedder := src.Embedder(ctx, effectiveTeam)
		llmClient := src.EnrichmentLLM(ctx, effectiveTeam)

		// --- Step 1: Fetch applicable rules ---
		applicableRules := []enrichContextRule{}
		var rawRules []storage.Rule

		if ruleStore, ok := store.(storage.RuleStore); ok {
			rules, err := ruleStore.GetApplicableRules(ctx, team, category, user)
			if err == nil { // degrade gracefully — return partial result on store/embed errors
				rawRules = rules
				for _, r := range rules {
					applicableRules = append(applicableRules, enrichContextRule{
						ID:      r.ID,
						Title:   r.Title,
						Content: r.Content,
						Scope:   string(r.Scope),
					})
				}
			}
		}

		// --- Step 2: Semantic knowledge search ---
		relevantKnowledge := []enrichContextKnowledge{}

		if embedder != nil {
			vec, err := embedder.Embed(ctx, prompt)
			if err == nil { // degrade gracefully — return partial result on store/embed errors
				const wantK = 5
				fetchK := wantK * 2
				if fetchK > 40 {
					fetchK = 40
				}
				results, err := store.SearchSimilar(ctx, vec, fetchK)
				if err == nil { // degrade gracefully — return partial result on store/embed errors
					// Team-scoping: filter out entries the caller cannot access.
					tc := auth.GetTeamContext(ctx)
					filtered := results[:0]
					for _, r := range results {
						if auth.CanAccess(tc, r.Entry.TeamID) {
							filtered = append(filtered, r)
						}
					}
					results = filtered
					if len(results) > wantK {
						results = results[:wantK]
					}
					for _, r := range results {
						relevantKnowledge = append(relevantKnowledge, enrichContextKnowledge{
							ID:      r.Entry.ID,
							Title:   r.Entry.Title,
							Score:   r.Score,
							Domain:  r.Entry.Domain,
							Content: r.Entry.Content,
						})
					}
				}
			}
		}

		// --- Step 3: Build improved prompt ---
		improvedPrompt := prompt

		// Apply rules as a numbered preamble (same pattern as buildEnhancedPrompt).
		if len(rawRules) > 0 {
			improvedPrompt = buildEnhancedPrompt(prompt, rawRules)
		}

		// If an LLM client is available and there is relevant knowledge, use it to further improve.
		if llmClient != nil && len(relevantKnowledge) > 0 {
			knowledgeBlock := ""
			for i, k := range relevantKnowledge {
				knowledgeBlock += fmt.Sprintf("Knowledge %d (score=%.3f, domain=%s):\nTitle: %s\n%s\n\n",
					i+1, k.Score, k.Domain, k.Title, k.Content)
			}
			llmPrompt := fmt.Sprintf(
				"You are a prompt engineering expert. Given a user prompt and relevant team knowledge entries, produce an improved version of the prompt that incorporates useful context from the knowledge entries. Return only the improved prompt text — no JSON wrapper, no explanation.\n\nOriginal prompt:\n%s\n\nRelevant team knowledge:\n%s\nReturn only the improved prompt.",
				improvedPrompt, knowledgeBlock,
			)
			if improved, err := llmClient.Complete(ctx, llmPrompt); err == nil && improved != "" {
				improvedPrompt = improved
			}
			// On LLM error, fall back to the rule-enhanced version — already set above.
		}

		// --- Step 4: Missing inputs checklist ---
		missingInputs := detectMissingInputs(prompt, team, category)

		// --- Build and return JSON response ---
		// (detectMissingInputs is defined below)
		resp := enrichContextResponse{
			ImprovedPrompt:    improvedPrompt,
			ApplicableRules:   applicableRules,
			RelevantKnowledge: relevantKnowledge,
			MissingInputs:     missingInputs,
		}

		// --- Publish live event (best-effort, nil-safe) ---
		fragment := live.CapFragment(prompt)
		publishEvent(bus, live.LiveEvent{
			Type:     live.TypeEnrichContext,
			TeamID:   teamID,
			Actor:    actor,
			Fragment: fragment,
		})

		// --- Persist activity to feed (best-effort, never fails the tool call) ---
		if err := store.RecordActivity(ctx, storage.ActivityEvent{
			EventType: live.TypeEnrichContext,
			ActorID:   actor.ID,
			Metadata: map[string]string{
				"fragment": fragment,
				"display":  actor.Display,
			},
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			slog.Warn("enrich_context: record activity", "err", err)
		}

		data, err := json.Marshal(resp)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("marshal response: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

// detectMissingInputs scans the prompt for common patterns where required context is absent.
// It checks both structural context (team/category) and content-specific gaps (e.g. a star
// rating mentioned without a complaint description).
func detectMissingInputs(prompt, team, category string) []string {
	var gaps []string
	lower := strings.ToLower(prompt)

	// Structural: no scoping context provided.
	if team == "" && category == "" {
		gaps = append(gaps, "Consider providing: domain/category context for scoped rule and knowledge lookup")
	}

	// Rating without complaint — e.g. "4 stars but no feedback given".
	// Triggered by: star symbols, "N star(s)", "N/5", "rating of N", standalone digits 1-5 near "star/rate/rating".
	hasRating := strings.ContainsAny(lower, "★☆") ||
		containsAny(lower, "1 star", "2 star", "3 star", "4 star", "5 star",
			"one star", "two star", "three star", "four star", "five star",
			"1/5", "2/5", "3/5", "4/5", "5/5", "rated ", "rating of", "gave a ", "gave it")
	hasComplaint := containsAny(lower,
		"complaint", "complain", "issue", "problem", "concern", "feedback",
		"dissatisfied", "unhappy", "unhappy", "frustrated", "broken", "wrong",
		"not working", "doesn't work", "failed", "error", "bug", "defect",
		"reason", "because", "but ", "however", "although", "even though")
	if hasRating && !hasComplaint {
		gaps = append(gaps, "Rating detected but no complaint or reason provided — ask the user what drove the score before proceeding")
	}

	// Vague request: very short prompt and no rules/knowledge were retrieved to compensate.
	if len(prompt) < 30 {
		gaps = append(gaps, "Prompt is very short — ask the user for more context before proceeding")
	}

	return gaps
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
