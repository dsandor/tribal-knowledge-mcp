package mcp

import (
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func NewMCPServer(store storage.Store, embedder embedding.Embedder) *server.MCPServer {
	s := server.NewMCPServer(
		"tribal-knowledge",
		"0.1.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_store",
			mcplib.WithDescription("Store a team knowledge entry (prompt, pattern, workflow, domain fact, or anti-pattern)"),
			mcplib.WithString("title", mcplib.Required(), mcplib.Description("Short descriptive title")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("Full content of the knowledge entry")),
			mcplib.WithString("type", mcplib.Required(), mcplib.Description("One of: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithString("domain", mcplib.Description("Domain tag (e.g. finance, legal, engineering)")),
			mcplib.WithString("description", mcplib.Description("When and why to use this entry")),
			mcplib.WithString("author", mcplib.Description("Author identifier")),
			mcplib.WithString("team", mcplib.Description("Team identifier")),
			mcplib.WithArray("tags", mcplib.Description("Additional tags")),
		),
		HandleKnowledgeStore(store, embedder),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_get",
			mcplib.WithDescription("Retrieve a knowledge entry by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID returned by knowledge_store or knowledge_list")),
		),
		HandleKnowledgeGet(store),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_list",
			mcplib.WithDescription("List knowledge entries with optional domain and type filters"),
			mcplib.WithString("domain", mcplib.Description("Filter by domain")),
			mcplib.WithString("type", mcplib.Description("Filter by type: prompt, pattern, workflow, domain_fact, anti_pattern")),
			mcplib.WithNumber("limit", mcplib.Description("Max entries to return (default 20, max 100)")),
		),
		HandleKnowledgeList(store),
	)

	s.AddTool(
		mcplib.NewTool("knowledge_delete",
			mcplib.WithDescription("Permanently delete a knowledge entry by its UUID"),
			mcplib.WithString("id", mcplib.Required(), mcplib.Description("Entry UUID to delete")),
		),
		HandleKnowledgeDelete(store),
	)

	return s
}
