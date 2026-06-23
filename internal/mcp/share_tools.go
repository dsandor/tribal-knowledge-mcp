package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/sharing"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterShareTools registers the cross-team knowledge sharing tools:
// knowledge_share (mint a single-use share link) and knowledge_import (consume
// one to copy a shared entry into the caller's team as a pending entry).
func RegisterShareTools(s *server.MCPServer, store storage.Store, src *aiconfig.Sources) {
	s.AddTool(
		mcplib.NewTool("knowledge_share",
			mcplib.WithDescription("Create a single-use share link for a knowledge entry so another team can import a copy."),
			mcplib.WithString("entry_id", mcplib.Required(), mcplib.Description("UUID of the knowledge entry to share")),
		),
		logTool("knowledge_share", HandleKnowledgeShare(store, src)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_import",
			mcplib.WithDescription("Import a shared knowledge entry into your team as a pending entry."),
			mcplib.WithString("share_id", mcplib.Required(), mcplib.Description("The share id (token) from a share link")),
		),
		logTool("knowledge_import", HandleKnowledgeImport(store, src)),
	)
}

// HandleKnowledgeShare mints a single-use cross-team share token for an entry
// the caller can access.
func HandleKnowledgeShare(store storage.Store, src *aiconfig.Sources) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		entryID := req.GetString("entry_id", "")
		if entryID == "" {
			return mcplib.NewToolResultError("entry_id is required"), nil
		}

		// Enforce team access; returns the entry or a ready-to-return error result.
		entry, errResult := fetchEntryForCaller(ctx, store, entryID)
		if errResult != nil {
			return errResult, nil
		}

		tc := auth.GetTeamContext(ctx)
		createdBy := tc.UserID
		if createdBy == "" {
			createdBy = tc.KeyID
		}

		share, err := sharing.CreateShare(ctx, store, entryID, entry.TeamID, createdBy)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("create share: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"share_id": share.ID,
			"url":      "/share/" + share.ID,
		})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

// HandleKnowledgeImport consumes a share token, copying the shared entry into the
// caller's team as a pending (curator-approval-pending), re-embedded entry.
func HandleKnowledgeImport(store storage.Store, src *aiconfig.Sources) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		shareID := req.GetString("share_id", "")
		if shareID == "" {
			return mcplib.NewToolResultError("share_id is required"), nil
		}

		tc := auth.GetTeamContext(ctx)
		destTeamID := tc.TeamID
		if destTeamID == "" {
			return mcplib.NewToolResultError("no team context — importing requires a team-scoped token"), nil
		}
		destUserID := tc.UserID
		if destUserID == "" {
			destUserID = tc.KeyID
		}

		newID, err := sharing.Import(ctx, store, src, shareID, destTeamID, destUserID)
		if errors.Is(err, sharing.ErrSameTeam) {
			return mcplib.NewToolResultText("This item already belongs to your team — no import needed."), nil
		}
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("import: %v", err)), nil
		}

		out, _ := json.Marshal(map[string]string{
			"imported_entry_id": newID,
			"status":            "pending",
			"note":              "Imported as a pending entry; it is awaiting curator approval before it appears in team results.",
		})
		return mcplib.NewToolResultText(string(out)), nil
	}
}
