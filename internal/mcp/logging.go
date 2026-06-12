package mcp

import (
	"context"
	"log/slog"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// logTool wraps an MCP tool handler with uniform invocation logging.
// It logs at Info level with fields: tool, team, duration_ms, status (ok | error),
// and err (only when a protocol-level error is returned).
func logTool(name string, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		start := time.Now()
		teamID, _ := resolveActorTeam(ctx)
		result, err := h(ctx, req)
		d := time.Since(start).Milliseconds()

		status := "ok"
		if err != nil || (result != nil && result.IsError) {
			status = "error"
		}

		if err != nil {
			slog.Info("mcp tool", "tool", name, "team", teamID, "duration_ms", d, "status", status, "err", err)
		} else {
			slog.Info("mcp tool", "tool", name, "team", teamID, "duration_ms", d, "status", status)
		}

		return result, err
	}
}
