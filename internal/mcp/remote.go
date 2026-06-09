package mcp

import (
	"log/slog"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/mark3labs/mcp-go/server"
)

// StartRemoteMCP starts a background HTTP server exposing the MCP server over
// StreamableHTTP transport. authStore is used to validate Bearer tokens so only
// authenticated clients can reach the MCP endpoint.
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
