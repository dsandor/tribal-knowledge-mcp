package mcp

import (
	"log/slog"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/mark3labs/mcp-go/server"
)

// RemoteMCPStore is the storage surface the remote MCP HTTP server needs:
// Bearer/session validation (auth.AuthStore) plus team-membership lookups for
// the X-Team-Id active-team override (auth.MembershipStore).
type RemoteMCPStore interface {
	auth.AuthStore
	auth.MembershipStore
}

// StartRemoteMCP starts a background HTTP server exposing the MCP server over
// Streamable HTTP transport (MCP spec 2025-03-26).
// Clients connect to http://<addr>/mcp (POST for JSON-RPC, GET for SSE stream).
// authStore is used to validate Bearer tokens and resolve team membership.
func StartRemoteMCP(mcpServer *server.MCPServer, addr, path string, authStore RemoteMCPStore) {
	if path == "" {
		path = "/mcp"
	}

	httpSrv := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath(path),
	)

	mux := http.NewServeMux()
	// Order: RequireAuth -> ActiveTeamMiddleware -> MCP handler.
	mux.Handle(path, auth.RequireAuth(authStore)(auth.ActiveTeamMiddleware(authStore)(httpSrv)))

	go func() {
		slog.Info("MCP HTTP server listening", "addr", addr, "path", path)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			slog.Error("MCP HTTP server error", "err", err)
		}
	}()
}
