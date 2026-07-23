package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dsandor/memory/internal/live"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterUsageTools adds the knowledge_use tool to an existing MCP server.
// bus may be nil; when non-nil a TypeKnowledgeUsed live event is published on
// each successful usage record.
func RegisterUsageTools(s *server.MCPServer, store storage.Store, bus live.EventBus) {
	s.AddTool(
		mcplib.NewTool("knowledge_use",
			mcplib.WithDescription("Call after accepting and applying a result from knowledge_search, enrich_context, or prompt_suggest. Pass the entry_id and the tool that produced it. This usage signal improves future retrieval ranking — call it every time you use an entry."),
			mcplib.WithString("entry_id", mcplib.Required(), mcplib.Description("The ID of the entry that was used")),
			mcplib.WithString("tool", mcplib.Required(), mcplib.Description("Which MCP tool produced the suggestion (e.g. \"prompt_suggest\")")),
			mcplib.WithNumber("selected_index", mcplib.Description("Which result index was selected (default 0)")),
			mcplib.WithString("user_id", mcplib.Description("Identifier for the user accepting the suggestion")),
			mcplib.WithString("session_id", mcplib.Description("Optional FT capture session_id from session_start")),
		),
		logTool("knowledge_use", HandleKnowledgeUse(store, bus)),
	)
}

// HandleKnowledgeUse returns a handler that records a usage event for a knowledge entry.
// bus may be nil; when non-nil a TypeKnowledgeUsed live event is published on success.
func HandleKnowledgeUse(store storage.Store, bus live.EventBus) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		entryID := req.GetString("entry_id", "")
		tool := req.GetString("tool", "")
		if entryID == "" || tool == "" {
			return mcplib.NewToolResultError("entry_id and tool are required"), nil
		}

		// Validate the ID belongs to a knowledge entry before hitting the FK constraint.
		// Rule IDs, cluster IDs, etc. are not valid here — each has its own tracking path.
		entry, err := store.GetEntry(ctx, entryID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return mcplib.NewToolResultError(fmt.Sprintf(
					"entry_id %q not found in the knowledge base — rule IDs and other non-knowledge IDs cannot be tracked via knowledge_use", entryID,
				)), nil
			}
			return mcplib.NewToolResultError(fmt.Sprintf("validate entry: %v", err)), nil
		}

		event := storage.UsageEvent{
			EntryID:       entryID,
			UserID:        req.GetString("user_id", ""),
			Tool:          tool,
			SelectedIndex: req.GetInt("selected_index", 0),
			CreatedAt:     time.Now().UTC(),
		}

		if err := store.RecordUsage(ctx, event); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("record usage: %v", err)), nil
		}

		if sid := req.GetString("session_id", ""); sid != "" {
			linkSessionKnowledge(ctx, store, sid, entryID, storage.FTKnowRetrieved)
		}

		// Publish live event (best-effort, nil-safe).
		teamID, actor := resolveActorTeam(ctx)
		title := ""
		if entry != nil {
			title = live.CapFragment(entry.Title)
		}
		publishEvent(bus, live.LiveEvent{
			Type:    live.TypeKnowledgeUsed,
			TeamID:  teamID,
			Actor:   actor,
			EntryID: entryID,
			Title:   title,
		})

		data, _ := json.Marshal(map[string]bool{"recorded": true})
		return mcplib.NewToolResultText(string(data)), nil
	}
}
