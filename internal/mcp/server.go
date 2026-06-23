package mcp

import (
	"context"
	"fmt"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const serverInstructions = `This server is the team's source of truth for how work should be done — rules, style guides, and accumulated knowledge.

BEFORE responding to ANY user request, call enrich_context with the user's message to pull applicable rules and relevant knowledge. Apply what it returns. If it reports missing_inputs, ask the user for that context before proceeding.

AFTER completing a non-trivial task, call knowledge_store to capture reusable learnings, and knowledge_use + knowledge_rate on the entries you relied on so the knowledge base self-improves over time.

When in doubt, consult the server — calls are cheap and idempotent.`

// knowledgeStoreBaseDescription is the statically-registered description for the
// knowledge_store tool. The tool filter augments it per-request with the team's
// effective content-size limit (see knowledgeStoreDescription).
const knowledgeStoreBaseDescription = "Call at the END of any non-trivial task to capture a reusable learning (prompt template, pattern, workflow, domain fact, or anti-pattern). Prefer storing over letting knowledge evaporate. Include concrete content and a clear description of when to use this entry. Skipping this means the team loses the insight. Inline #hashtags in the title or content are automatically extracted as tags."

// knowledgeStoreDescription builds the knowledge_store tool description,
// telegraphing the effective per-team content-size behavior so client LLMs do
// not pre-trim or split content themselves.
func knowledgeStoreDescription(maxTokens int) string {
	return fmt.Sprintf("%s Content of any length is accepted — items larger than ~%d tokens are automatically split into linked chunks internally and remain fully searchable as a single entry, so do not pre-trim or split content yourself.", knowledgeStoreBaseDescription, maxTokens)
}

// knowledgeStoreToolFilter returns a mcp-go ToolFilterFunc that rewrites the
// knowledge_store tool's description to reflect the requesting team's effective
// chunk-size limit. All other tools are returned unchanged.
func knowledgeStoreToolFilter(src *aiconfig.Sources) server.ToolFilterFunc {
	return func(ctx context.Context, tools []mcplib.Tool) []mcplib.Tool {
		if src == nil {
			return tools
		}
		teamID, _ := resolveActorTeam(ctx)
		maxTokens := src.ChunkConfig(ctx, teamID).MaxTokens
		if maxTokens <= 0 {
			return tools
		}
		out := make([]mcplib.Tool, len(tools))
		for i, t := range tools {
			if t.Name == "knowledge_store" {
				t.Description = knowledgeStoreDescription(maxTokens)
			}
			out[i] = t
		}
		return out
	}
}

func NewMCPServer(store storage.Store, src *aiconfig.Sources, bus ...live.EventBus) *server.MCPServer {
	var eventBus live.EventBus
	if len(bus) > 0 {
		eventBus = bus[0]
	}

	s := server.NewMCPServer(
		"tribal-knowledge",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
		server.WithResourceCapabilities(true, false),
		server.WithInstructions(serverInstructions),
		server.WithToolFilter(knowledgeStoreToolFilter(src)),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_store",
			mcplib.WithDescription(knowledgeStoreBaseDescription),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Short descriptive title")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("Full content of the knowledge entry")),
			mcplib.WithString("type", mcplib.Required(), mcplib.Description("One of: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithString("domain", mcplib.Description("Domain tag (e.g. finance, legal, engineering)")),
			mcplib.WithString("description", mcplib.Description("When and why to use this entry")),
			mcplib.WithString("author", mcplib.Description("Author identifier")),
			mcplib.WithString("team", mcplib.Description("Team identifier")),
			mcplib.WithArray("tags", mcplib.Description("Additional tags (inline #hashtags in content are also extracted automatically)")),
			mcplib.WithBoolean("dry_run", mcplib.Description("If true, validate and preview the entry without storing it. Returns the entry that would be stored including its content_hash.")),
		),
		logTool("knowledge_store", HandleKnowledgeStore(store, src, eventBus)),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_get",
			mcplib.WithDescription("Retrieve a knowledge entry by UUID — use when you need the full content of an entry returned by knowledge_search or enrich_context."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID returned by knowledge_store or knowledge_list")),
		),
		logTool("knowledge_get", HandleKnowledgeGet(store)),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_list",
			mcplib.WithDescription("Browse knowledge entries with optional domain and type filters. Use when exploring what the team knows about a topic rather than searching by semantic similarity."),
			mcplib.WithString("domain", mcplib.Description("Filter by domain")),
			mcplib.WithString("type", mcplib.Description("Filter by type: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithNumber("limit", mcplib.Description("Max entries to return (default 20, max 100)")),
		),
		logTool("knowledge_list", HandleKnowledgeList(store)),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_delete",
			mcplib.WithDescription("Permanently delete a knowledge entry — call only after confirming with the user that an entry is outdated or incorrect."),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID to delete")),
		),
		logTool("knowledge_delete", HandleKnowledgeDelete(store)),
	)

	return s
}

// RegisterAnalysisTools adds the cluster_list and analysis_status tools to an existing MCP server.
func RegisterAnalysisTools(s *server.MCPServer, store storage.AnalysisStore) {
	s.AddTool(
		mcplib.NewTool("cluster_list",
			mcplib.WithDescription("List knowledge clusters produced by the analysis pipeline, with LLM-generated summaries"),
		),
		logTool("cluster_list", HandleClusterList(store)),
	)
	s.AddTool(
		mcplib.NewTool("analysis_status",
			mcplib.WithDescription("Show the latest analysis pipeline run status and dataset snapshot info"),
		),
		logTool("analysis_status", HandleAnalysisStatus(store)),
	)
}

// RegisterAgentTools adds agent management tools and a use_agent prompt to an existing MCP server.
func RegisterAgentTools(s *server.MCPServer, store storage.AgentStore) {
	s.AddTool(
		mcplib.NewTool("agent_list",
			mcplib.WithDescription("List all AI agents generated from knowledge clusters, with their domain, version, and status"),
		),
		logTool("agent_list", HandleAgentList(store)),
	)
	s.AddTool(
		mcplib.NewTool("agent_get",
			mcplib.WithDescription("Get a specific agent by id or domain name"),
			mcplib.WithString("id", mcplib.Description("Agent UUID (optional if domain provided)")),
			mcplib.WithString("domain", mcplib.Description("Domain name, e.g. finance (optional if id provided)")),
		),
		logTool("agent_get", HandleAgentGet(store)),
	)
	s.AddTool(
		mcplib.NewTool("agent_publish",
			mcplib.WithDescription("Approve a draft agent — sets its status to published so it can be served to clients"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Agent UUID to publish")),
		),
		logTool("agent_publish", HandleAgentPublish(store)),
	)
	s.AddTool(
		mcplib.NewTool("agent_export",
			mcplib.WithDescription("Export an agent as a Claude subagent .md file, plain text .txt, or structured .json"),
			mcplib.WithString("id", mcplib.Description("Agent UUID (optional if domain provided)")),
			mcplib.WithString("domain", mcplib.Description("Domain name (optional if id provided)")),
			mcplib.WithString("format", mcplib.Description("Export format: md, txt, or json (default: md)")),
		),
		logTool("agent_export", HandleAgentExport(store)),
	)

	s.AddPrompt(
		mcplib.NewPrompt("use_agent",
			mcplib.WithPromptDescription("Get the system prompt for a domain's published agent to use as context"),
			mcplib.WithArgument("domain",
				mcplib.ArgumentDescription("Domain name, e.g. finance, legal, engineering"),
				mcplib.RequiredArgument(),
			),
		),
		func(ctx context.Context, req mcplib.GetPromptRequest) (*mcplib.GetPromptResult, error) {
			domain := req.Params.Arguments["domain"]
			teamID, _ := resolveActorTeam(ctx)
			a, err := store.GetAgentByDomain(ctx, domain, teamID)
			if err != nil || a == nil || a.Status != storage.AgentStatusPublished {
				msg := fmt.Sprintf("No published agent found for domain: %s", domain)
				return &mcplib.GetPromptResult{
					Description: msg,
					Messages: []mcplib.PromptMessage{
						{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: msg}},
					},
				}, nil
			}
			tc := auth.GetTeamContext(ctx)
			if !auth.CanAccess(tc, a.TeamID) {
				msg := fmt.Sprintf("No published agent found for domain: %s", domain)
				return &mcplib.GetPromptResult{
					Description: msg,
					Messages: []mcplib.PromptMessage{
						{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: msg}},
					},
				}, nil
			}
			return &mcplib.GetPromptResult{
				Description: fmt.Sprintf("%s domain agent v%d (%s)", a.Domain, a.Version, string(a.Status)),
				Messages: []mcplib.PromptMessage{
					{Role: mcplib.RoleUser, Content: mcplib.TextContent{Type: "text", Text: a.SystemPrompt}},
				},
			}, nil
		},
	)
}

// RegisterRuleTools adds the rule CRUD and prompt_enhance tools to an existing MCP server.
func RegisterRuleTools(s *server.MCPServer, store storage.RuleStore) {
	s.AddTool(
		mcplib.NewTool("rule_store",
			mcplib.WithDescription("Create a rule that governs how prompts are processed, worded, or structured"),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Short descriptive title")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("The rule text — what to do, what steps to follow, how to word output")),
			mcplib.WithString("scope", mcplib.Required(), mcplib.Description("Scope level: 'team', 'category', or 'user'")),
			mcplib.WithString("scope_value", mcplib.Description("Team ID, domain/category name, or user identifier this rule applies to")),
			mcplib.WithNumber("priority", mcplib.Description("Rule priority — higher = applied first within scope (default 0)")),
			mcplib.WithString("author", mcplib.Description("Author identifier")),
		),
		logTool("rule_store", HandleRuleStore(store)),
	)
	s.AddTool(
		mcplib.NewTool("rule_get",
			mcplib.WithDescription("Retrieve a rule by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Rule UUID")),
		),
		logTool("rule_get", HandleRuleGet(store)),
	)
	s.AddTool(
		mcplib.NewTool("rule_list",
			mcplib.WithDescription("List rules with optional scope and scope_value filters"),
			mcplib.WithString("scope", mcplib.Description("Filter by scope: 'team', 'category', or 'user'")),
			mcplib.WithString("scope_value", mcplib.Description("Filter by scope value (team ID, category, or user)")),
			mcplib.WithNumber("limit", mcplib.Description("Max rules to return (default 20)")),
		),
		logTool("rule_list", HandleRuleList(store)),
	)
	s.AddTool(
		mcplib.NewTool("rule_update",
			mcplib.WithDescription("Update a rule — call rule_get first to fetch current values, then supply all fields with your modifications"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Rule UUID to update")),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Updated title")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("Updated rule content")),
			mcplib.WithString("scope", mcplib.Description("Updated scope: 'team', 'category', or 'user' (default 'team')")),
			mcplib.WithString("scope_value", mcplib.Description("Updated scope value")),
			mcplib.WithNumber("priority", mcplib.Description("Updated priority (default 0)")),
			mcplib.WithString("enabled", mcplib.Description("'true' or 'false' (default 'true')")),
			mcplib.WithString("author", mcplib.Description("Updated author")),
		),
		logTool("rule_update", HandleRuleUpdate(store)),
	)
	s.AddTool(
		mcplib.NewTool("rule_delete",
			mcplib.WithDescription("Permanently delete a rule by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Rule UUID to delete")),
		),
		logTool("rule_delete", HandleRuleDelete(store)),
	)
	s.AddTool(
		mcplib.NewTool("prompt_enhance",
			mcplib.WithDescription("Call at the start of a turn to prepend all applicable rules for the given team/category/user context. Safe to call always — returns the prompt unchanged if no rules match. Prefer enrich_context for a full context pack."),
			mcplib.WithString("prompt", mcplib.Required(), mcplib.Description("The original prompt to enhance")),
			mcplib.WithString("team", mcplib.Description("Team identifier — fetches team-scoped rules")),
			mcplib.WithString("category", mcplib.Description("Category/domain — fetches category-scoped rules")),
			mcplib.WithString("user", mcplib.Description("User identifier — fetches user-scoped rules")),
		),
		logTool("prompt_enhance", HandlePromptEnhance(store)),
	)
}
