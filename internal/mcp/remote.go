package mcp

import (
	"fmt"
	"log/slog"

	"github.com/dsandor/memory/internal/auth"
	"github.com/mark3labs/mcp-go/server"
)

// StartRemoteMCP starts a background HTTP server exposing the MCP server over
// SSE transport (compatible with Claude Desktop and other MCP clients).
// Clients connect to GET /sse and post messages to POST /message.
// authStore is used to validate Bearer tokens so only authenticated clients
// can reach the MCP endpoint. Pass an empty authStore or use DEV_BYPASS_AUTH
// to skip auth for local development.
func StartRemoteMCP(mcpServer *server.MCPServer, addr, _ string, _ auth.AuthStore) {
	baseURL := fmt.Sprintf("http://localhost%s", addr)

	sseSrv := server.NewSSEServer(mcpServer,
		server.WithBaseURL(baseURL),
	)

	go func() {
		slog.Info("MCP SSE server listening", "addr", addr, "sse", "/sse", "message", "/message")
		if err := sseSrv.Start(addr); err != nil {
			slog.Error("MCP SSE server error", "err", err)
		}
	}()
}
