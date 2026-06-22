package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- minimal fakes for constructing *aiconfig.Sources (internal package) ---

type filterFakeSettingsStore struct{}

func (filterFakeSettingsStore) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	return &storage.TeamSettings{}, nil
}

type filterFakeEmbedProvider struct{}

func (filterFakeEmbedProvider) Embedder(_, _ string) embedding.Embedder { return nil }

type filterFakeLLMProvider struct{}

func (filterFakeLLMProvider) Client(_, _ string) llm.Client { return nil }
func (filterFakeLLMProvider) Ollama(_, _ string) llm.Client { return nil }

func filterTestSources(maxTokens int) *aiconfig.Sources {
	resolver := aiconfig.NewResolver(filterFakeSettingsStore{}, aiconfig.EnvDefaults{
		EmbeddingMaxTokens: maxTokens,
	})
	return &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         filterFakeLLMProvider{},
		Embed:       filterFakeEmbedProvider{},
		DefaultTeam: "test",
	}
}

func TestKnowledgeStoreDescription(t *testing.T) {
	desc := knowledgeStoreDescription(8192)
	if !strings.Contains(desc, "8192") {
		t.Errorf("description should contain the configured token count 8192, got: %q", desc)
	}
	if !strings.Contains(desc, "automatically split into linked chunks") {
		t.Errorf("description missing key chunking phrase, got: %q", desc)
	}
	if !strings.Contains(desc, "do not pre-trim or split content yourself") {
		t.Errorf("description missing anti-pretrim phrase, got: %q", desc)
	}
}

func TestKnowledgeStoreToolFilter(t *testing.T) {
	const maxTokens = 4096
	filter := knowledgeStoreToolFilter(filterTestSources(maxTokens))

	const otherDesc = "an unrelated tool description that must not change"
	in := []mcplib.Tool{
		{Name: "knowledge_store", Description: knowledgeStoreBaseDescription},
		{Name: "knowledge_get", Description: otherDesc},
	}

	out := filter(context.Background(), in)
	if len(out) != len(in) {
		t.Fatalf("filter changed tool count: got %d want %d", len(out), len(in))
	}

	var ks, other *mcplib.Tool
	for i := range out {
		switch out[i].Name {
		case "knowledge_store":
			ks = &out[i]
		case "knowledge_get":
			other = &out[i]
		}
	}
	if ks == nil || other == nil {
		t.Fatalf("expected both tools in output, got %+v", out)
	}

	if !strings.Contains(ks.Description, "4096") {
		t.Errorf("knowledge_store description should contain configured token count 4096, got: %q", ks.Description)
	}
	if !strings.Contains(ks.Description, "automatically split into linked chunks") {
		t.Errorf("knowledge_store description missing chunking phrase, got: %q", ks.Description)
	}
	if other.Description != otherDesc {
		t.Errorf("other tool description was modified: got %q want %q", other.Description, otherDesc)
	}
}
