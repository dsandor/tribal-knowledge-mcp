package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

func sampleAgent() *storage.Agent {
	return &storage.Agent{
		ID:           "agent-1",
		Domain:       "finance",
		Version:      2,
		Status:       storage.AgentStatusPublished,
		SystemPrompt: "You are a financial analysis expert.",
		Instructions: "Use DCF valuation.\nCite sources.",
		AntiPatterns: "Do not guess earnings.",
		SourceRefs:   []string{"cluster-1"},
		ClusterID:    "cluster-1",
	}
}

func TestExport_MD(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "md")
	if !strings.Contains(out, "---") {
		t.Error("md export should have YAML frontmatter delimiters")
	}
	if !strings.Contains(out, "domain: finance") {
		t.Errorf("md export missing domain in frontmatter, got:\n%s", out)
	}
	if !strings.Contains(out, "You are a financial analysis expert.") {
		t.Error("md export should contain system prompt in body")
	}
	if !strings.Contains(out, "version: 2") {
		t.Errorf("md export missing version in frontmatter, got:\n%s", out)
	}
	if !strings.Contains(out, "status: published") {
		t.Errorf("md export missing status in frontmatter, got:\n%s", out)
	}
}

func TestExport_TXT(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "txt")
	if !strings.Contains(out, "You are a financial analysis expert.") {
		t.Error("txt export should contain system prompt")
	}
	if !strings.Contains(out, "Use DCF valuation.") {
		t.Error("txt export should contain instructions")
	}
}

func TestExport_JSON(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "json")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json export is not valid JSON: %v\n%s", err, out)
	}
	if parsed["domain"] != "finance" {
		t.Errorf("json export domain = %v, want finance", parsed["domain"])
	}
	v, ok := parsed["version"].(float64)
	if !ok {
		t.Fatalf("json export version field missing or wrong type, got: %T", parsed["version"])
	}
	if v != 2 {
		t.Errorf("json export version = %v, want 2", v)
	}
}

func TestExport_UnknownFormat(t *testing.T) {
	a := sampleAgent()
	out := Export(a, "xml")
	if !strings.Contains(out, "unsupported format") {
		t.Errorf("unknown format should return error message, got: %q", out)
	}
}
