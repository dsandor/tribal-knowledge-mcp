package web

import "testing"

func TestBuildMCPURL(t *testing.T) {
	cases := []struct {
		name   string
		scheme string
		host   string
		addr   string
		path   string
		want   string
	}{
		{"port-only addr", "http", "myhost:8080", ":8081", "/mcp", "http://myhost:8081/mcp"},
		{"wildcard addr", "http", "myhost", "0.0.0.0:8081", "/mcp", "http://myhost:8081/mcp"},
		{"https scheme", "https", "kb.example.com:443", ":8081", "/mcp", "https://kb.example.com:8081/mcp"},
		{"custom path", "http", "myhost:8080", ":9090", "/mcp/v1", "http://myhost:9090/mcp/v1"},
		{"disabled when addr empty", "http", "myhost:8080", "", "/mcp", ""},
		{"path without leading slash", "http", "myhost", ":8081", "mcp", "http://myhost:8081/mcp"},
		{"empty path defaults to /mcp", "http", "myhost", ":8081", "", "http://myhost:8081/mcp"},
		{"ipv6 request host", "http", "[::1]:8080", ":8081", "/mcp", "http://[::1]:8081/mcp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildMCPURL(c.scheme, c.host, c.addr, c.path)
			if got != c.want {
				t.Errorf("buildMCPURL(%q,%q,%q,%q) = %q, want %q", c.scheme, c.host, c.addr, c.path, got, c.want)
			}
		})
	}
}
