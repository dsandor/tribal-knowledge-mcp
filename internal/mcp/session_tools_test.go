package mcp_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestSessionToolsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	store, err := storage.NewSQLiteStore(path, 768)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	start := internalmcp.HandleSessionStart(store)
	res, err := start(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{
				"task_summary": "build ft export",
				"domain":       "engineering",
				"client":       "opencode",
				"project":      "memory",
			},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("start: err=%v res=%+v", err, res)
	}
	var startOut map[string]any
	if err := json.Unmarshal([]byte(toolText(res)), &startOut); err != nil {
		t.Fatalf("parse start: %v", err)
	}
	sid, _ := startOut["session_id"].(string)
	if sid == "" {
		t.Fatal("missing session_id")
	}

	turn := internalmcp.HandleSessionTurn(store)
	res, err = turn(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{
				"session_id": sid,
				"role":       "user",
				"kind":       "message",
				"content":    "implement export-train",
			},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("turn: err=%v res=%+v", err, res)
	}

	res, err = turn(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{
				"session_id": sid,
				"role":       "assistant",
				"kind":       "message",
				"content":    "implemented",
			},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("turn2: err=%v res=%+v", err, res)
	}

	pref := internalmcp.HandleSessionPrefer(store)
	res, err = pref(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{
				"session_id":     sid,
				"chosen_text":    "final",
				"rejected_text":  "draft",
				"source":         "user_edit",
			},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("prefer: err=%v res=%+v", err, res)
	}

	complete := internalmcp.HandleSessionComplete(store)
	res, err = complete(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{
				"session_id":      sid,
				"outcome_rating":  5.0,
				"status":          "completed",
			},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("complete: err=%v res=%+v", err, res)
	}

	sess, err := store.GetFTSession(ctx, sid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.Status != storage.FTSessionCompleted {
		t.Fatalf("status=%s", sess.Status)
	}
	turns, _ := store.ListFTTurns(ctx, sid)
	if len(turns) != 2 {
		t.Fatalf("turns=%d", len(turns))
	}
}

func toolText(res *mcplib.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcplib.TextContent); ok {
		return tc.Text
	}
	return ""
}
