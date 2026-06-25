package aiconfig

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
	"github.com/dsandor/memory/internal/storage"
)

type fakeCompleteClient struct{}

func (f *fakeCompleteClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "", nil
}

type fakeLLMProvider struct {
	anthropicCalls []string
	ollamaCalls    []string
}

func (f *fakeLLMProvider) Client(apiKey, model string) llm.Client {
	f.anthropicCalls = append(f.anthropicCalls, apiKey+"|"+model)
	if apiKey == "" {
		return nil
	}
	return &fakeCompleteClient{}
}

func (f *fakeLLMProvider) Ollama(url, model string) llm.Client {
	f.ollamaCalls = append(f.ollamaCalls, url+"|"+model)
	if url == "" || model == "" {
		return nil
	}
	return &fakeCompleteClient{}
}

type fakeSettingsStore struct{ settings *storage.TeamSettings }

func (f *fakeSettingsStore) GetTeamSettings(ctx context.Context, teamID string) (*storage.TeamSettings, error) {
	if f.settings == nil {
		return nil, storage.ErrNotFound
	}
	return f.settings, nil
}

func newTestSources(saved *storage.TeamSettings, env EnvDefaults) (*Sources, *fakeLLMProvider) {
	p := &fakeLLMProvider{}
	return &Sources{
		Resolver: NewResolver(&fakeSettingsStore{settings: saved}, env),
		LLM:      p,
	}, p
}

func TestSourcesDefaultProviderIsAnthropic(t *testing.T) {
	src, p := newTestSources(nil, EnvDefaults{AnthropicAPIKey: "k", AnthropicModel: "claude-x"})
	c := src.AnalysisLLM(context.Background(), "t1")
	if c == nil {
		t.Fatal("expected anthropic client")
	}
	if len(p.anthropicCalls) != 1 || p.anthropicCalls[0] != "k|claude-x" {
		t.Fatalf("anthropic calls = %v", p.anthropicCalls)
	}
	if len(p.ollamaCalls) != 0 {
		t.Fatalf("unexpected ollama calls: %v", p.ollamaCalls)
	}
}

func TestSourcesOllamaProvider(t *testing.T) {
	saved := &storage.TeamSettings{LLMProvider: "ollama", OllamaURL: "http://o", OllamaLLMModel: "llama3.1"}
	src, p := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k"})
	if c := src.AnalysisLLM(context.Background(), "t1"); c == nil {
		t.Fatal("expected ollama client")
	}
	if c := src.AgentLLM(context.Background(), "t1"); c == nil {
		t.Fatal("expected ollama agent client")
	}
	if len(p.ollamaCalls) != 2 || p.ollamaCalls[0] != "http://o|llama3.1" {
		t.Fatalf("ollama calls = %v", p.ollamaCalls)
	}
	if len(p.anthropicCalls) != 0 {
		t.Fatalf("anthropic must not be called: %v", p.anthropicCalls)
	}
}

func TestSourcesOllamaUnconfiguredReturnsNil(t *testing.T) {
	saved := &storage.TeamSettings{LLMProvider: "ollama", OllamaURL: "http://o"} // no chat model
	src, _ := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k"})
	if c := src.AnalysisLLM(context.Background(), "t1"); c != nil {
		t.Fatal("expected nil for unconfigured ollama (no silent anthropic fallback)")
	}
}

func TestSourcesImprovementModelPinning(t *testing.T) {
	src, p := newTestSources(nil, EnvDefaults{AnthropicAPIKey: "k", AnthropicModel: "claude-x"})
	src.ImprovementLLM(context.Background(), "t1")
	if len(p.anthropicCalls) != 1 || p.anthropicCalls[0] != "k|claude-haiku-4-5-20251001" {
		t.Fatalf("improvement anthropic calls = %v, want pinned haiku", p.anthropicCalls)
	}

	saved := &storage.TeamSettings{LLMProvider: "ollama", OllamaURL: "http://o", OllamaLLMModel: "llama3.1"}
	src2, p2 := newTestSources(saved, EnvDefaults{})
	src2.ImprovementLLM(context.Background(), "t1")
	if len(p2.ollamaCalls) != 1 || p2.ollamaCalls[0] != "http://o|llama3.1" {
		t.Fatalf("improvement ollama calls = %v, want team chat model", p2.ollamaCalls)
	}
}

// TestLLMForTouchpointExplicitOllama: saved AITouchpoints{"analysis": {Provider:"ollama", Model:"m1"}}
// + OllamaURL set → Ollama called "url|m1"; Anthropic not called.
func TestLLMForTouchpointExplicitOllama(t *testing.T) {
	saved := &storage.TeamSettings{
		OllamaURL: "http://o",
		AITouchpoints: map[string]storage.AITouchpoint{
			"analysis": {Provider: "ollama", Model: "m1"},
		},
	}
	src, p := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k"})
	c := src.LLMForTouchpoint(context.Background(), "t1", TouchpointAnalysis)
	if c == nil {
		t.Fatal("expected ollama client")
	}
	if len(p.ollamaCalls) != 1 || p.ollamaCalls[0] != "http://o|m1" {
		t.Fatalf("ollama calls = %v, want [http://o|m1]", p.ollamaCalls)
	}
	if len(p.anthropicCalls) != 0 {
		t.Fatalf("anthropic must not be called: %v", p.anthropicCalls)
	}
}

// TestLLMForTouchpointExplicitOllamaNoModel: entry {Provider:"ollama"} (no model) + team
// OllamaLLMModel "chat1" → Ollama "url|chat1".
func TestLLMForTouchpointExplicitOllamaNoModel(t *testing.T) {
	saved := &storage.TeamSettings{
		OllamaURL:      "http://o",
		OllamaLLMModel: "chat1",
		AITouchpoints: map[string]storage.AITouchpoint{
			"analysis": {Provider: "ollama"},
		},
	}
	src, p := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k"})
	c := src.LLMForTouchpoint(context.Background(), "t1", TouchpointAnalysis)
	if c == nil {
		t.Fatal("expected ollama client")
	}
	if len(p.ollamaCalls) != 1 || p.ollamaCalls[0] != "http://o|chat1" {
		t.Fatalf("ollama calls = %v, want [http://o|chat1]", p.ollamaCalls)
	}
}

// TestLLMForTouchpointExplicitAnthropic: {"agents": {Provider:"anthropic", Model:"claude-z"}}
// → Client "key|claude-z".
func TestLLMForTouchpointExplicitAnthropic(t *testing.T) {
	saved := &storage.TeamSettings{
		AITouchpoints: map[string]storage.AITouchpoint{
			"agents": {Provider: "anthropic", Model: "claude-z"},
		},
	}
	src, p := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "mykey"})
	c := src.LLMForTouchpoint(context.Background(), "t1", TouchpointAgents)
	if c == nil {
		t.Fatal("expected anthropic client")
	}
	if len(p.anthropicCalls) != 1 || p.anthropicCalls[0] != "mykey|claude-z" {
		t.Fatalf("anthropic calls = %v, want [mykey|claude-z]", p.anthropicCalls)
	}
	if len(p.ollamaCalls) != 0 {
		t.Fatalf("unexpected ollama calls: %v", p.ollamaCalls)
	}
}

// TestLLMForTouchpointFallbackChain: no touchpoint entry + team llm_provider "" + env key →
// Client called with the touchpoint's role-default model.
func TestLLMForTouchpointFallbackChain(t *testing.T) {
	tests := []struct {
		touchpoint string
		wantModel  func(cfg *EffectiveConfig) string
	}{
		{TouchpointAnalysis, func(cfg *EffectiveConfig) string { return cfg.AnthropicModel.Effective }},
		{TouchpointEnrichment, func(cfg *EffectiveConfig) string { return cfg.AnthropicModel.Effective }},
		{TouchpointAgents, func(cfg *EffectiveConfig) string { return cfg.AgentModel.Effective }},
		{TouchpointImprovement, func(_ *EffectiveConfig) string { return improvementHaikuModel }},
	}

	env := EnvDefaults{
		AnthropicAPIKey: "envkey",
		AnthropicModel:  "claude-analysis",
		AgentModel:      "claude-agent",
	}
	for _, tc := range tests {
		t.Run(tc.touchpoint, func(t *testing.T) {
			src, p := newTestSources(nil, env)
			// Compute expected model from a resolved config
			cfg, _ := src.Resolver.Effective(context.Background(), "t1")
			wantModel := tc.wantModel(cfg)

			c := src.LLMForTouchpoint(context.Background(), "t1", tc.touchpoint)
			if c == nil {
				t.Fatalf("touchpoint %q: expected client, got nil", tc.touchpoint)
			}
			wantCall := "envkey|" + wantModel
			if len(p.anthropicCalls) != 1 || p.anthropicCalls[0] != wantCall {
				t.Fatalf("touchpoint %q: anthropic calls = %v, want [%s]", tc.touchpoint, p.anthropicCalls, wantCall)
			}
		})
	}
}

// TestEnrichmentLLMUsesEnrichmentTouchpoint: {"enrichment": {Provider:"ollama", Model:"m2"}}
// → EnrichmentLLM resolves Ollama "url|m2" while AnalysisLLM (unset) stays anthropic.
func TestEnrichmentLLMUsesEnrichmentTouchpoint(t *testing.T) {
	saved := &storage.TeamSettings{
		OllamaURL: "http://o",
		AITouchpoints: map[string]storage.AITouchpoint{
			"enrichment": {Provider: "ollama", Model: "m2"},
		},
	}
	src, p := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k", AnthropicModel: "claude-x"})

	enrichClient := src.EnrichmentLLM(context.Background(), "t1")
	if enrichClient == nil {
		t.Fatal("expected ollama client for enrichment")
	}
	if len(p.ollamaCalls) != 1 || p.ollamaCalls[0] != "http://o|m2" {
		t.Fatalf("ollama calls = %v, want [http://o|m2]", p.ollamaCalls)
	}

	p.anthropicCalls = nil // reset
	p.ollamaCalls = nil
	analysisClient := src.AnalysisLLM(context.Background(), "t1")
	if analysisClient == nil {
		t.Fatal("expected anthropic client for analysis")
	}
	if len(p.anthropicCalls) != 1 {
		t.Fatalf("analysis anthropic calls = %v, want 1", p.anthropicCalls)
	}
	if len(p.ollamaCalls) != 0 {
		t.Fatalf("analysis must not call ollama: %v", p.ollamaCalls)
	}
}

// TestLLMFingerprintPerTouchpoint: with enrichment→ollama and analysis unset,
// LLMFingerprint for "enrichment" == "ollama|url|m2" and "analysis" == "anthropic|<analysis model>".
func TestLLMFingerprintPerTouchpoint(t *testing.T) {
	saved := &storage.TeamSettings{
		OllamaURL: "http://o",
		AITouchpoints: map[string]storage.AITouchpoint{
			"enrichment": {Provider: "ollama", Model: "m2"},
		},
	}
	src, _ := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k", AnthropicModel: "claude-x"})

	enrichFP := src.LLMFingerprint(context.Background(), "t1", TouchpointEnrichment)
	wantEnrich := "ollama|http://o|m2"
	if enrichFP != wantEnrich {
		t.Fatalf("enrichment fingerprint = %q, want %q", enrichFP, wantEnrich)
	}

	analysisFP := src.LLMFingerprint(context.Background(), "t1", TouchpointAnalysis)
	wantAnalysis := "anthropic|claude-x"
	if analysisFP != wantAnalysis {
		t.Fatalf("analysis fingerprint = %q, want %q", analysisFP, wantAnalysis)
	}
}

// recordingClient is a fake llm.Client that records Complete calls.
type recordingClient struct {
	calls []string
}

func (r *recordingClient) Complete(ctx context.Context, prompt string) (string, error) {
	r.calls = append(r.calls, prompt)
	return "recorded", nil
}

// recordingLLMProvider returns a *recordingClient so we can verify passthrough.
type recordingLLMProvider struct {
	client *recordingClient
}

func (r *recordingLLMProvider) Client(apiKey, model string) llm.Client {
	if apiKey == "" {
		return nil
	}
	return r.client
}

func (r *recordingLLMProvider) Ollama(url, model string) llm.Client {
	if url == "" || model == "" {
		return nil
	}
	return r.client
}

// TestLLMForTouchpointWrapsWithLogging: resolved client is NOT the raw *recordingClient
// (it's wrapped in a LoggingClient) but Complete passes through to it.
func TestLLMForTouchpointWrapsWithLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	rec := &recordingClient{}
	p := &recordingLLMProvider{client: rec}
	src := &Sources{
		Resolver: NewResolver(&fakeSettingsStore{}, EnvDefaults{AnthropicAPIKey: "k", AnthropicModel: "claude-x"}),
		LLM:      p,
	}

	c := src.LLMForTouchpoint(context.Background(), "t1", TouchpointAnalysis)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	// The returned client must NOT be the raw recording client — it should be wrapped.
	if _, isRaw := c.(*recordingClient); isRaw {
		t.Fatal("expected wrapped client, got raw *recordingClient (wrapping not applied)")
	}

	// But Complete must pass through to the inner recording client.
	out, err := c.Complete(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "recorded" {
		t.Fatalf("got %q, want 'recorded'", out)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "hello" {
		t.Fatalf("inner client calls = %v, want ['hello']", rec.calls)
	}

	// A Debug log line should have been emitted for the successful call.
	log := buf.String()
	if !strings.Contains(log, "llm call") {
		t.Errorf("expected 'llm call' in log, got: %s", log)
	}
}

// TestLLMUnconfiguredWarns: ollama selected but no model → returns UNTYPED nil
// (c == nil must be true for the llm.Client interface value!) and captured log
// contains "llm unconfigured" with touchpoint+team.
func TestLLMUnconfiguredWarns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Ollama provider but no chat model → provider returns nil.
	saved := &storage.TeamSettings{LLMProvider: "ollama", OllamaURL: "http://o"} // OllamaLLMModel intentionally empty
	src, _ := newTestSources(saved, EnvDefaults{AnthropicAPIKey: "k"})

	c := src.LLMForTouchpoint(context.Background(), "t1", TouchpointAnalysis)
	// CRITICAL: must be untyped nil so that callers' c == nil check works.
	if c != nil {
		t.Fatalf("expected nil for unconfigured ollama, got %v (%T)", c, c)
	}

	log := buf.String()
	if !strings.Contains(log, "llm unconfigured") {
		t.Errorf("expected 'llm unconfigured' warn in log, got: %s", log)
	}
	if !strings.Contains(log, "touchpoint=analysis") {
		t.Errorf("expected touchpoint=analysis in log, got: %s", log)
	}
	if !strings.Contains(log, "team=t1") {
		t.Errorf("expected team=t1 in log, got: %s", log)
	}
}

// --- Embedder provider-selection test doubles ---

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) { return nil, nil }
func (stubEmbedder) Ping(_ context.Context) error                         { return nil }

// recordingEmbedProvider records which accessor was called and with what args.
type recordingEmbedProvider struct {
	ollamaCalls []string // "url|model"
	openAICalls []string // "baseURL|apiKey|model"
}

func (r *recordingEmbedProvider) Embedder(url, model string) embedding.Embedder {
	r.ollamaCalls = append(r.ollamaCalls, url+"|"+model)
	return stubEmbedder{}
}

func (r *recordingEmbedProvider) OpenAIEmbedder(baseURL, apiKey, model string) embedding.Embedder {
	r.openAICalls = append(r.openAICalls, baseURL+"|"+apiKey+"|"+model)
	return stubEmbedder{}
}

// fakeEmbedConfigStore returns a configurable embedding config (or error).
type fakeEmbedConfigStore struct {
	cfg *storage.EmbeddingConfig
	err error
}

func (f *fakeEmbedConfigStore) GetEmbeddingConfig(_ context.Context) (*storage.EmbeddingConfig, error) {
	return f.cfg, f.err
}

// TestEmbedderOpenAI: provider "openai" routes to OpenAIEmbedder with the
// config's base URL, API key, and model.
func TestEmbedderOpenAI(t *testing.T) {
	rec := &recordingEmbedProvider{}
	src := &Sources{
		Embed: rec,
		EmbedConfig: &fakeEmbedConfigStore{cfg: &storage.EmbeddingConfig{
			Provider:      "openai",
			Model:         "text-embedding-3-small",
			OpenAIAPIKey:  "sk-test",
			OpenAIBaseURL: "https://api.openai.com",
			OllamaURL:     "http://o", // must be ignored for openai
		}},
	}
	e := src.Embedder(context.Background(), "any-team")
	if e == nil {
		t.Fatal("expected non-nil embedder for provider=openai")
	}
	if len(rec.openAICalls) != 1 || rec.openAICalls[0] != "https://api.openai.com|sk-test|text-embedding-3-small" {
		t.Fatalf("openai calls = %v", rec.openAICalls)
	}
	if len(rec.ollamaCalls) != 0 {
		t.Fatalf("ollama must not be called for openai: %v", rec.ollamaCalls)
	}
}

// TestEmbedderOllama: provider "ollama" routes to Embedder with the config's
// Ollama URL and model.
func TestEmbedderOllama(t *testing.T) {
	rec := &recordingEmbedProvider{}
	src := &Sources{
		Embed: rec,
		EmbedConfig: &fakeEmbedConfigStore{cfg: &storage.EmbeddingConfig{
			Provider:      "ollama",
			Model:         "nomic-embed",
			OllamaURL:     "http://ollama:11434",
			OpenAIBaseURL: "https://api.openai.com", // must be ignored for ollama
			OpenAIAPIKey:  "sk-ignored",
		}},
	}
	e := src.Embedder(context.Background(), "any-team")
	if e == nil {
		t.Fatal("expected non-nil embedder for provider=ollama")
	}
	if len(rec.ollamaCalls) != 1 || rec.ollamaCalls[0] != "http://ollama:11434|nomic-embed" {
		t.Fatalf("ollama calls = %v", rec.ollamaCalls)
	}
	if len(rec.openAICalls) != 0 {
		t.Fatalf("openai must not be called for ollama: %v", rec.openAICalls)
	}
}

// TestEmbedderNilOrErrorConfig: a nil config, a read error, an unknown provider,
// and a nil EmbedConfig source all yield a nil embedder (fail-soft) without
// touching the EmbedProvider.
func TestEmbedderNilOrErrorConfig(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		rec := &recordingEmbedProvider{}
		src := &Sources{Embed: rec, EmbedConfig: &fakeEmbedConfigStore{cfg: nil}}
		if e := src.Embedder(context.Background(), "t"); e != nil {
			t.Fatalf("expected nil embedder for nil config, got %v", e)
		}
		if len(rec.ollamaCalls)+len(rec.openAICalls) != 0 {
			t.Fatalf("provider must not be called: ollama=%v openai=%v", rec.ollamaCalls, rec.openAICalls)
		}
	})
	t.Run("read error", func(t *testing.T) {
		rec := &recordingEmbedProvider{}
		src := &Sources{Embed: rec, EmbedConfig: &fakeEmbedConfigStore{err: errors.New("boom")}}
		if e := src.Embedder(context.Background(), "t"); e != nil {
			t.Fatalf("expected nil embedder on read error, got %v", e)
		}
	})
	t.Run("unknown provider", func(t *testing.T) {
		rec := &recordingEmbedProvider{}
		src := &Sources{Embed: rec, EmbedConfig: &fakeEmbedConfigStore{cfg: &storage.EmbeddingConfig{Provider: "mystery"}}}
		if e := src.Embedder(context.Background(), "t"); e != nil {
			t.Fatalf("expected nil embedder for unknown provider, got %v", e)
		}
		if len(rec.ollamaCalls)+len(rec.openAICalls) != 0 {
			t.Fatalf("provider must not be called for unknown provider")
		}
	})
	t.Run("nil EmbedConfig source", func(t *testing.T) {
		rec := &recordingEmbedProvider{}
		src := &Sources{Embed: rec} // EmbedConfig intentionally nil
		if e := src.Embedder(context.Background(), "t"); e != nil {
			t.Fatalf("expected nil embedder when EmbedConfig is nil, got %v", e)
		}
	})
}
