package mcp_test

import (
	"context"
	"testing"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/llm"
	internalmcp "github.com/dsandor/memory/internal/mcp"
	"github.com/dsandor/memory/internal/storage"
)

// recordingLLMProvider distinguishes Anthropic vs Ollama calls and records each.
type recordingLLMProvider struct {
	anthropicCalls []string
	ollamaCalls    []string
}

func (r *recordingLLMProvider) Client(apiKey, model string) llm.Client {
	r.anthropicCalls = append(r.anthropicCalls, apiKey+"|"+model)
	if apiKey == "" {
		return nil
	}
	return &fakeLLMClient{resp: "improved prompt"}
}

func (r *recordingLLMProvider) Ollama(url, model string) llm.Client {
	r.ollamaCalls = append(r.ollamaCalls, url+"|"+model)
	if url == "" || model == "" {
		return nil
	}
	return &fakeLLMClient{resp: "improved prompt"}
}

// settingsStoreWithTouchpoints returns a SettingsStore that always returns the
// given TeamSettings (regardless of teamID).
type settingsStoreWithTouchpoints struct {
	settings *storage.TeamSettings
}

func (s *settingsStoreWithTouchpoints) GetTeamSettings(_ context.Context, _ string) (*storage.TeamSettings, error) {
	if s.settings == nil {
		return nil, storage.ErrNotFound
	}
	return s.settings, nil
}

// newTouchpointSources builds a *aiconfig.Sources with a recordingLLMProvider
// and the given saved TeamSettings + env defaults. Returns the Sources and the
// recording provider so the test can assert on call counts.
func newTouchpointSources(saved *storage.TeamSettings, env aiconfig.EnvDefaults, embedder interface {
	Embed(context.Context, string) ([]float32, error)
}) (*aiconfig.Sources, *recordingLLMProvider) {
	p := &recordingLLMProvider{}
	resolver := aiconfig.NewResolver(&settingsStoreWithTouchpoints{settings: saved}, env)
	src := &aiconfig.Sources{
		Resolver:    resolver,
		LLM:         p,
		Embed:       &fakeEmbedProvider{e: embedder},
		DefaultTeam: "test",
	}
	return src, p
}

// ---------------------------------------------------------------------------
// TestEnrichmentTouchpointRouting
//
// Sources whose saved settings set AITouchpoints{"enrichment":{Provider:"ollama",
// Model:"m2"}} with OllamaURL set, and anthropic env key present.
// Calling the enrich_context handler (and prompt_suggest handler) must result
// in an Ollama call (enrichment touchpoint honored) and zero Anthropic calls
// from those handlers.
// ---------------------------------------------------------------------------

func TestEnrichmentTouchpointRouting(t *testing.T) {
	saved := &storage.TeamSettings{
		OllamaURL: "http://ollama-test",
		AITouchpoints: map[string]storage.AITouchpoint{
			"enrichment": {Provider: "ollama", Model: "m2"},
		},
	}
	env := aiconfig.EnvDefaults{
		AnthropicAPIKey: "env-key",
		AnthropicModel:  "claude-x",
	}

	// Use a real embedder mock that returns a non-nil vector so the handler
	// reaches the LLM call path (it only calls Complete when there is
	// relevant knowledge AND an LLM client is available).
	embedder := &mockEmbedder{embedding: []float32{0.1, 0.2, 0.3}}
	store := &mockStore{
		entries: []storage.KnowledgeEntry{
			{ID: "k1", Title: "Team Rule", TeamID: "test", Content: "use bullet points"},
		},
	}

	t.Run("enrich_context", func(t *testing.T) {
		src, rec := newTouchpointSources(saved, env, embedder)
		handler := internalmcp.HandleEnrichContext(store, src, nil)
		req := callReq("prompt", "analyze quarterly earnings in detail please")

		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool returned error: %s", textContent(result))
		}

		if len(rec.ollamaCalls) == 0 {
			t.Fatal("expected at least one Ollama call for enrichment touchpoint, got none")
		}
		// The Ollama call must use the configured URL and model.
		gotCall := rec.ollamaCalls[0]
		wantCall := "http://ollama-test|m2"
		if gotCall != wantCall {
			t.Errorf("ollama call = %q, want %q", gotCall, wantCall)
		}
		if len(rec.anthropicCalls) != 0 {
			t.Errorf("expected zero Anthropic calls from enrich_context, got: %v", rec.anthropicCalls)
		}
	})

	t.Run("prompt_suggest", func(t *testing.T) {
		src, rec := newTouchpointSources(saved, env, embedder)
		handler := internalmcp.HandlePromptSuggest(store, src)
		req := callReq("prompt", "analyze the earnings report for actionable insights")

		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("tool returned error: %s", textContent(result))
		}

		if len(rec.ollamaCalls) == 0 {
			t.Fatal("expected at least one Ollama call for enrichment touchpoint, got none")
		}
		gotCall := rec.ollamaCalls[0]
		wantCall := "http://ollama-test|m2"
		if gotCall != wantCall {
			t.Errorf("ollama call = %q, want %q", gotCall, wantCall)
		}
		if len(rec.anthropicCalls) != 0 {
			t.Errorf("expected zero Anthropic calls from prompt_suggest, got: %v", rec.anthropicCalls)
		}
	})
}
