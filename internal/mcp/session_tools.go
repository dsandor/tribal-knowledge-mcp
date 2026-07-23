package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterSessionTools adds fine-tune session capture tools.
func RegisterSessionTools(s *server.MCPServer, store storage.FTSessionStore) {
	s.AddTool(
		mcplib.NewTool("session_start",
			mcplib.WithDescription("Start a fine-tune capture session at the beginning of a non-trivial task. Returns session_id — pass it to session_turn, session_link_knowledge, session_prefer, session_complete, and optionally to enrich_context/knowledge_store/knowledge_use/knowledge_rate."),
			mcplib.WithString("task_summary", mcplib.Required(), mcplib.Description("Short summary of the user task/goal")),
			mcplib.WithString("project", mcplib.Description("Project, repo name, or working directory")),
			mcplib.WithString("domain", mcplib.Description("Domain tag (e.g. engineering, reviews)")),
			mcplib.WithString("client", mcplib.Description("Client name: opencode | claude-code | cursor | api")),
			mcplib.WithString("user_id", mcplib.Description("User identifier (defaults to authenticated actor)")),
			mcplib.WithBoolean("train_eligible", mcplib.Description("If false, exclude from train export (default true)")),
			mcplib.WithString("metadata_json", mcplib.Description("Optional JSON object of extra metadata (model, branch, etc.)")),
		),
		logTool("session_start", HandleSessionStart(store)),
	)
	s.AddTool(
		mcplib.NewTool("session_turn",
			mcplib.WithDescription("Append a turn to an open capture session (user message, assistant reply, tool call/result, enrich step, or revision)."),
			mcplib.WithString("session_id", mcplib.Required(), mcplib.Description("Session UUID from session_start")),
			mcplib.WithString("role", mcplib.Required(), mcplib.Description("user | assistant | tool | system_inject")),
			mcplib.WithString("kind", mcplib.Required(), mcplib.Description("message | tool_call | tool_result | enrich | revision")),
			mcplib.WithString("content", mcplib.Required(), mcplib.Description("Turn body text")),
			mcplib.WithString("model", mcplib.Description("Model that produced assistant turns")),
			mcplib.WithString("tool_name", mcplib.Description("Tool name for tool_call/tool_result")),
			mcplib.WithString("parent_turn_id", mcplib.Description("Parent tool_call turn id for tool_result")),
			mcplib.WithString("agent_id", mcplib.Description("Published agent id if used")),
			mcplib.WithArray("entry_ids", mcplib.Description("Knowledge entry IDs used this turn")),
			mcplib.WithArray("rule_ids", mcplib.Description("Rule IDs applied this turn")),
			mcplib.WithNumber("token_estimate", mcplib.Description("Optional token estimate")),
		),
		logTool("session_turn", HandleSessionTurn(store)),
	)
	s.AddTool(
		mcplib.NewTool("session_link_knowledge",
			mcplib.WithDescription("Link a knowledge entry to a capture session (retrieved, stored, rated, or agent_source)."),
			mcplib.WithString("session_id", mcplib.Required(), mcplib.Description("Session UUID")),
			mcplib.WithString("entry_id", mcplib.Required(), mcplib.Description("Knowledge entry UUID")),
			mcplib.WithString("role", mcplib.Required(), mcplib.Description("retrieved | stored | rated | agent_source")),
		),
		logTool("session_link_knowledge", HandleSessionLinkKnowledge(store)),
	)
	s.AddTool(
		mcplib.NewTool("session_prefer",
			mcplib.WithDescription("Record a preference pair for DPO: chosen (accepted) vs rejected (prior draft). Call when the user edits your output or picks a better version."),
			mcplib.WithString("session_id", mcplib.Required(), mcplib.Description("Session UUID")),
			mcplib.WithString("chosen_text", mcplib.Required(), mcplib.Description("Accepted final text")),
			mcplib.WithString("rejected_text", mcplib.Description("Prior/worse draft (strongly preferred for DPO)")),
			mcplib.WithString("source", mcplib.Description("user_edit | rate_up | rate_down | a_b_pick | knowledge_store (default user_edit)")),
			mcplib.WithString("prompt_turn_id", mcplib.Description("Turn id of the user/enriched prompt")),
			mcplib.WithString("turn_id", mcplib.Description("Assistant turn being judged")),
			mcplib.WithString("entry_id", mcplib.Description("Related knowledge entry if any")),
			mcplib.WithNumber("rating", mcplib.Description("Optional 1-5 rating of the chosen text")),
		),
		logTool("session_prefer", HandleSessionPrefer(store)),
	)
	s.AddTool(
		mcplib.NewTool("session_complete",
			mcplib.WithDescription("Close a capture session after the task finishes. Pass overall outcome_rating 1-5 when possible."),
			mcplib.WithString("session_id", mcplib.Required(), mcplib.Description("Session UUID")),
			mcplib.WithNumber("outcome_rating", mcplib.Description("Overall session quality 1-5")),
			mcplib.WithString("outcome_note", mcplib.Description("Optional note about the outcome")),
			mcplib.WithString("status", mcplib.Description("completed (default) or abandoned")),
		),
		logTool("session_complete", HandleSessionComplete(store)),
	)
}

func HandleSessionStart(store storage.FTSessionStore) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		summary := strings.TrimSpace(req.GetString("task_summary", ""))
		if summary == "" {
			return mcplib.NewToolResultError("task_summary is required"), nil
		}
		teamID, actor := resolveActorTeam(ctx)
		userID := req.GetString("user_id", "")
		if userID == "" {
			userID = actor.ID
		}
		trainEligible := true
		if args := req.GetArguments(); args != nil {
			if v, ok := args["train_eligible"]; ok {
				switch b := v.(type) {
				case bool:
					trainEligible = b
				}
			}
		}
		sess := storage.FTSession{
			TeamID:          teamID,
			UserID:          userID,
			Client:          req.GetString("client", ""),
			Project:         req.GetString("project", ""),
			TaskSummary:     summary,
			Domain:          req.GetString("domain", ""),
			Status:          storage.FTSessionOpen,
			TrainEligible:   trainEligible,
			RedactionStatus: "raw",
			MetadataJSON:    req.GetString("metadata_json", ""),
		}
		id, err := store.CreateFTSession(ctx, sess)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("session_start: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{
			"session_id":     id,
			"status":         storage.FTSessionOpen,
			"train_eligible": trainEligible,
		})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

func HandleSessionTurn(store storage.FTSessionStore) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID := req.GetString("session_id", "")
		role := req.GetString("role", "")
		kind := req.GetString("kind", "")
		content := req.GetString("content", "")
		if sessionID == "" || role == "" || kind == "" || content == "" {
			return mcplib.NewToolResultError("session_id, role, kind, and content are required"), nil
		}
		if !storage.ValidFTRole(role) {
			return mcplib.NewToolResultError("role must be user | assistant | tool | system_inject"), nil
		}
		if !storage.ValidFTKind(kind) {
			return mcplib.NewToolResultError("kind must be message | tool_call | tool_result | enrich | revision"), nil
		}
		if _, err := store.GetFTSession(ctx, sessionID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return mcplib.NewToolResultError("session not found"), nil
			}
			return mcplib.NewToolResultError(fmt.Sprintf("get session: %v", err)), nil
		}
		entryIDs := stringsFromArgs(req.GetArguments(), "entry_ids")
		ruleIDs := stringsFromArgs(req.GetArguments(), "rule_ids")
		turn := storage.FTTurn{
			SessionID:     sessionID,
			Seq:           -1, // auto-assign next seq
			Role:          role,
			Kind:          kind,
			Content:       content,
			Model:         req.GetString("model", ""),
			ToolName:      req.GetString("tool_name", ""),
			ParentTurnID:  req.GetString("parent_turn_id", ""),
			AgentID:       req.GetString("agent_id", ""),
			EntryIDs:      entryIDs,
			RuleIDs:       ruleIDs,
			TokenEstimate: req.GetInt("token_estimate", 0),
		}
		id, err := store.AddFTTurn(ctx, turn)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("session_turn: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{"turn_id": id, "session_id": sessionID})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

func HandleSessionLinkKnowledge(store storage.FTSessionStore) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID := req.GetString("session_id", "")
		entryID := req.GetString("entry_id", "")
		role := req.GetString("role", "")
		if sessionID == "" || entryID == "" || role == "" {
			return mcplib.NewToolResultError("session_id, entry_id, and role are required"), nil
		}
		if !storage.ValidFTKnowRole(role) {
			return mcplib.NewToolResultError("role must be retrieved | stored | rated | agent_source"), nil
		}
		if _, err := store.GetFTSession(ctx, sessionID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return mcplib.NewToolResultError("session not found"), nil
			}
			return mcplib.NewToolResultError(fmt.Sprintf("get session: %v", err)), nil
		}
		if err := store.LinkFTSessionKnowledge(ctx, sessionID, entryID, role); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("link: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{"linked": true, "session_id": sessionID, "entry_id": entryID, "role": role})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

func HandleSessionPrefer(store storage.FTSessionStore) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID := req.GetString("session_id", "")
		chosen := req.GetString("chosen_text", "")
		if sessionID == "" || chosen == "" {
			return mcplib.NewToolResultError("session_id and chosen_text are required"), nil
		}
		source := req.GetString("source", storage.FTPrefUserEdit)
		if source == "" {
			source = storage.FTPrefUserEdit
		}
		if !storage.ValidFTPrefSource(source) {
			return mcplib.NewToolResultError("source must be user_edit | rate_up | rate_down | a_b_pick | knowledge_store"), nil
		}
		if _, err := store.GetFTSession(ctx, sessionID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return mcplib.NewToolResultError("session not found"), nil
			}
			return mcplib.NewToolResultError(fmt.Sprintf("get session: %v", err)), nil
		}
		_, actor := resolveActorTeam(ctx)
		var rating *int
		if r := req.GetInt("rating", 0); r >= 1 && r <= 5 {
			rating = &r
		}
		p := storage.FTPreference{
			SessionID:    sessionID,
			TurnID:       req.GetString("turn_id", ""),
			PromptTurnID: req.GetString("prompt_turn_id", ""),
			ChosenText:   chosen,
			RejectedText: req.GetString("rejected_text", ""),
			Source:       source,
			Rating:       rating,
			EntryID:      req.GetString("entry_id", ""),
			UserID:       actor.ID,
		}
		id, err := store.AddFTPreference(ctx, p)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("session_prefer: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{"preference_id": id, "session_id": sessionID})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

func HandleSessionComplete(store storage.FTSessionStore) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		sessionID := req.GetString("session_id", "")
		if sessionID == "" {
			return mcplib.NewToolResultError("session_id is required"), nil
		}
		status := req.GetString("status", storage.FTSessionCompleted)
		if status == "" {
			status = storage.FTSessionCompleted
		}
		if status != storage.FTSessionCompleted && status != storage.FTSessionAbandoned {
			return mcplib.NewToolResultError("status must be completed or abandoned"), nil
		}
		var rating *int
		if r := req.GetInt("outcome_rating", 0); r >= 1 && r <= 5 {
			rating = &r
		}
		if err := store.CompleteFTSession(ctx, sessionID, rating, req.GetString("outcome_note", ""), status); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return mcplib.NewToolResultError("session not found"), nil
			}
			return mcplib.NewToolResultError(fmt.Sprintf("session_complete: %v", err)), nil
		}
		out, _ := json.Marshal(map[string]any{"session_id": sessionID, "status": status, "completed": true})
		return mcplib.NewToolResultText(string(out)), nil
	}
}

// stringsFromArgs extracts a string slice from MCP tool args (array of strings).
func stringsFromArgs(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// linkSessionKnowledge is a best-effort helper used by other tools when session_id is passed.
func linkSessionKnowledge(ctx context.Context, store storage.Store, sessionID, entryID, role string) {
	if sessionID == "" || entryID == "" {
		return
	}
	fs, ok := store.(storage.FTSessionStore)
	if !ok {
		return
	}
	_ = fs.LinkFTSessionKnowledge(ctx, sessionID, entryID, role)
}

// appendSessionTurn is a best-effort helper to log a turn when session_id is present.
func appendSessionTurn(ctx context.Context, store storage.Store, turn storage.FTTurn) {
	if turn.SessionID == "" {
		return
	}
	fs, ok := store.(storage.FTSessionStore)
	if !ok {
		return
	}
	turn.Seq = -1 // always auto-assign
	_, _ = fs.AddFTTurn(ctx, turn)
}
