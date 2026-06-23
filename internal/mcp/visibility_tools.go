package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// callerActorID resolves the calling caller's stable effective identity (user
// id, else API key id, else "local"). It always returns a non-empty id, so the
// visibility-management tools work for team tokens, stdio and dev/no-auth
// setups — each operates on that identity's own per-caller suppression rules.
func callerActorID(ctx context.Context) string {
	return auth.GetTeamContext(ctx).EffectiveActorID()
}

// validMuteKind reports whether kind is a valid rule type for mute/unmute.
func validMuteKind(kind string) bool {
	switch kind {
	case "author", "tag", "domain":
		return true
	default:
		return false
	}
}

// RegisterVisibilityTools registers the per-user knowledge visibility management
// tools. Every tool operates only on the calling user's own suppression rules
// and requires a user-scoped token.
func RegisterVisibilityTools(s *server.MCPServer, store storage.Store) {
	s.AddTool(
		mcplib.NewTool("knowledge_hide",
			mcplib.WithDescription("Hide a specific knowledge entry from your own searches and lists. Affects only you, not your teammates. Requires a user-scoped token."),
			mcplib.WithString("entry_id", mcplib.Required(), mcplib.Description("UUID of the knowledge entry to hide from your view")),
		),
		logTool("knowledge_hide", HandleKnowledgeHide(store)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_unhide",
			mcplib.WithDescription("Un-hide a previously hidden knowledge entry so it appears in your searches and lists again. Affects only you."),
			mcplib.WithString("entry_id", mcplib.Required(), mcplib.Description("UUID of the knowledge entry to un-hide")),
		),
		logTool("knowledge_unhide", HandleKnowledgeUnhide(store)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_mute",
			mcplib.WithDescription("Mute all entries by an author, or with a given tag or domain, from your own view. Affects only you, not your teammates. Requires a user-scoped token."),
			mcplib.WithString("kind", mcplib.Required(), mcplib.Description("What to mute by: 'author', 'tag', or 'domain'")),
			mcplib.WithString("value", mcplib.Required(), mcplib.Description("The author identifier, tag, or domain to mute")),
		),
		logTool("knowledge_mute", HandleKnowledgeMute(store)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_unmute",
			mcplib.WithDescription("Remove a previously set mute on an author, tag, or domain so those entries appear again. Affects only you."),
			mcplib.WithString("kind", mcplib.Required(), mcplib.Description("What was muted: 'author', 'tag', or 'domain'")),
			mcplib.WithString("value", mcplib.Required(), mcplib.Description("The author identifier, tag, or domain to unmute")),
		),
		logTool("knowledge_unmute", HandleKnowledgeUnmute(store)),
	)
	s.AddTool(
		mcplib.NewTool("knowledge_visibility",
			mcplib.WithDescription("Show what you have hidden or muted — lists your current per-user suppression rules. Affects only your own view."),
		),
		logTool("knowledge_visibility", HandleKnowledgeVisibility(store)),
	)
}

// HandleKnowledgeHide adds an "item" suppression rule for the calling user.
func HandleKnowledgeHide(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		userID := callerActorID(ctx)
		entryID := req.GetString("entry_id", "")
		if entryID == "" {
			return mcplib.NewToolResultError("entry_id is required"), nil
		}
		if _, err := store.AddVisibilityRule(ctx, userID, "item", entryID); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("hide entry: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Entry %s hidden from your view.", entryID)), nil
	}
}

// HandleKnowledgeUnhide deletes the "item" suppression rule for the calling user.
func HandleKnowledgeUnhide(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		userID := callerActorID(ctx)
		entryID := req.GetString("entry_id", "")
		if entryID == "" {
			return mcplib.NewToolResultError("entry_id is required"), nil
		}
		if err := store.DeleteVisibilityRule(ctx, userID, "item", entryID); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("unhide entry: %v", err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Entry %s is visible to you again.", entryID)), nil
	}
}

// HandleKnowledgeMute adds an author/tag/domain suppression rule for the caller.
func HandleKnowledgeMute(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		userID := callerActorID(ctx)
		kind := req.GetString("kind", "")
		if !validMuteKind(kind) {
			return mcplib.NewToolResultError("kind must be one of: 'author', 'tag', 'domain'"), nil
		}
		value := req.GetString("value", "")
		if value == "" {
			return mcplib.NewToolResultError("value is required"), nil
		}
		if _, err := store.AddVisibilityRule(ctx, userID, kind, value); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("mute %s: %v", kind, err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Muted %s %q from your view.", kind, value)), nil
	}
}

// HandleKnowledgeUnmute deletes an author/tag/domain suppression rule.
func HandleKnowledgeUnmute(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		userID := callerActorID(ctx)
		kind := req.GetString("kind", "")
		if !validMuteKind(kind) {
			return mcplib.NewToolResultError("kind must be one of: 'author', 'tag', 'domain'"), nil
		}
		value := req.GetString("value", "")
		if value == "" {
			return mcplib.NewToolResultError("value is required"), nil
		}
		if err := store.DeleteVisibilityRule(ctx, userID, kind, value); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("unmute %s: %v", kind, err)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Unmuted %s %q.", kind, value)), nil
	}
}

// HandleKnowledgeVisibility lists the calling user's current suppression rules.
func HandleKnowledgeVisibility(store storage.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		userID := callerActorID(ctx)
		rules, err := store.ListVisibilityRules(ctx, userID)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("list visibility rules: %v", err)), nil
		}
		type ruleView struct {
			RuleType  string `json:"rule_type"`
			Value     string `json:"value"`
			CreatedAt string `json:"created_at"`
		}
		out := make([]ruleView, 0, len(rules))
		for _, r := range rules {
			out = append(out, ruleView{
				RuleType:  r.RuleType,
				Value:     r.Value,
				CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		data, err := json.Marshal(out)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("encode rules: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}
