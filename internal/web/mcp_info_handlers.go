package web

import (
	"net"
	"net/http"
	"strings"
)

// buildMCPURL assembles the externally reachable remote-MCP endpoint URL from
// the web request's host (which the browser could reach, so its hostname is a
// good default) and the MCP listener's configured addr/path.
// Returns "" when mcpAddr is empty (remote MCP disabled).
func buildMCPURL(scheme, requestHost, mcpAddr, mcpPath string) string {
	if mcpAddr == "" {
		return ""
	}
	host := requestHost
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		host = h
	}
	port := strings.TrimPrefix(mcpAddr, ":")
	if _, p, err := net.SplitHostPort(mcpAddr); err == nil {
		port = p
	}
	if mcpPath == "" {
		mcpPath = "/mcp"
	} else if !strings.HasPrefix(mcpPath, "/") {
		mcpPath = "/" + mcpPath
	}
	return scheme + "://" + net.JoinHostPort(host, port) + mcpPath
}

// handleMCPInfo reports whether the remote (Streamable HTTP) MCP endpoint is
// enabled and, if so, its URL — used by the UI to render client-setup snippets.
func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.trustXFF {
		if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
			// May be a comma-separated list behind chained proxies
			// (e.g. "https, http"); the first entry is the client-facing hop.
			first := strings.TrimSpace(strings.SplitN(xfp, ",", 2)[0])
			if first != "" {
				scheme = first
			}
		}
	}
	url := buildMCPURL(scheme, r.Host, s.mcpHTTPAddr, s.mcpHTTPPath)
	writeJSON(w, map[string]any{
		"http_enabled": s.mcpHTTPAddr != "",
		"url":          url,
	})
}
