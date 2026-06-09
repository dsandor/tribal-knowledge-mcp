package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/dsandor/memory/internal/storage"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

func TestGenerate_ParsesLLMResponse(t *testing.T) {
	mock := &mockLLM{response: `{
		"system_prompt": "You are a financial analysis assistant.",
		"instructions": "Always use DCF for valuation.\nCite sources.",
		"anti_patterns": "Do not guess earnings without data."
	}`}
	cluster := storage.Cluster{
		ID:      "c1",
		Domain:  "finance",
		Title:   "Finance Patterns",
		Summary: "Common finance analysis patterns.",
	}
	entries := []storage.KnowledgeEntry{
		{Title: "DCF Analysis", Content: "Use discounted cash flow..."},
		{Title: "Earnings Patterns", Content: "Look for EPS growth..."},
	}

	a, err := Generate(context.Background(), mock, cluster, entries)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.Domain != "finance" {
		t.Errorf("domain = %q, want finance", a.Domain)
	}
	if a.SystemPrompt != "You are a financial analysis assistant." {
		t.Errorf("system_prompt = %q", a.SystemPrompt)
	}
	if a.Instructions == "" {
		t.Error("instructions is empty")
	}
	if a.AntiPatterns == "" {
		t.Error("anti_patterns is empty")
	}
	if a.ClusterID != "c1" {
		t.Errorf("cluster_id = %q, want c1", a.ClusterID)
	}
	if len(a.SourceRefs) == 0 || a.SourceRefs[0] != "c1" {
		t.Errorf("source_refs = %v, want [c1]", a.SourceRefs)
	}
	if a.Status != storage.AgentStatusDraft {
		t.Errorf("status = %q, want draft", a.Status)
	}
	if a.Version != 1 {
		t.Errorf("version = %d, want 1", a.Version)
	}
}

func TestGenerate_MarkdownFence(t *testing.T) {
	mock := &mockLLM{response: "```json\n{\"system_prompt\":\"SP\",\"instructions\":\"I\",\"anti_patterns\":\"AP\"}\n```"}
	cluster := storage.Cluster{ID: "c2", Domain: "ops", Title: "Ops", Summary: "ops patterns"}
	entries := []storage.KnowledgeEntry{{Title: "E", Content: "C"}}

	a, err := Generate(context.Background(), mock, cluster, entries)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if a.SystemPrompt != "SP" {
		t.Errorf("system_prompt = %q, want SP", a.SystemPrompt)
	}
}

func TestGenerate_LLMError(t *testing.T) {
	mock := &mockLLM{err: fmt.Errorf("network error")}
	cluster := storage.Cluster{Domain: "finance", Title: "F", Summary: "S"}
	_, err := Generate(context.Background(), mock, cluster, nil)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestGenerate_BadJSON(t *testing.T) {
	mock := &mockLLM{response: "Sorry, I cannot do that."}
	cluster := storage.Cluster{Domain: "d", Title: "T", Summary: "S"}
	_, err := Generate(context.Background(), mock, cluster, nil)
	if err == nil {
		t.Fatal("expected error for unparseable LLM response")
	}
}
