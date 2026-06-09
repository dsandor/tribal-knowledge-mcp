package mcp

import (
	"log/slog"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/mark3labs/mcp-go/server"
)

// StartRemoteMCP starts a background HTTP server exposing the MCP server over
// Streamable HTTP transport (MCP spec 2025-03-26).
// Clients connect to http://<addr>/mcp (POST for JSON-RPC, GET for SSE stream).
// authStore is used to validate Bearer tokens.
func StartRemoteMCP(mcpServer *server.MCPServer, addr, path string, authStore auth.AuthStore) {
	if path == "" {
		path = "/mcp"
	}

	httpSrv := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath(path),
	)

	mux := http.NewServeMux()
	mux.Handle(path, auth.RequireAuth(authStore)(httpSrv))

	go func() {
		slog.Info("MCP HTTP server listening", "addr", addr, "path", path)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			slog.Error("MCP HTTP server error", "err", err)
		}
	}()
}
