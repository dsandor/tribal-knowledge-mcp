package aiconfig

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

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
