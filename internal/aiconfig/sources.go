package aiconfig

import (
	"context"
	"log/slog"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/llm"
)

// LLMProvider is the narrow interface required by Sources to obtain cached LLM clients.
// *llm.Provider satisfies this.
type LLMProvider interface {
	Client(apiKey, model string) llm.Client
	Ollama(url, model string) llm.Client
}

// EmbedProvider is the narrow interface required by Sources to obtain cached Embedders.
// *embedding.Provider satisfies this.
type EmbedProvider interface {
	Embedder(url, model string) embedding.Embedder
}

// Sources resolves ready-to-use AI clients from the effective configuration on
// each call, so saved team settings take effect immediately without a restart.
type Sources struct {
	Resolver *Resolver
	LLM      LLMProvider
	Embed    EmbedProvider
	// DefaultTeam is the team ID applied as a fallback by every method
	// (AnalysisLLM, AgentLLM, ImprovementLLM, Embedder) when the caller
	// supplies an empty teamID. This is typically set at startup to the
	// single-team value derived from the environment (e.g. stdio MCP
	// connections that carry no team context in the request).
	DefaultTeam string
}

// Touchpoint names for per-usage AI configuration.
const (
	TouchpointAnalysis    = "analysis"
	TouchpointAgents      = "agents"
	TouchpointImprovement = "improvement"
	TouchpointEnrichment  = "enrichment"
)

const improvementHaikuModel = "claude-haiku-4-5-20251001"

// resolveTouchpoint returns the provider and model for a touchpoint, applying
// touchpoint-level overrides first, then falling back to the team-level defaults.
// This logic is shared by LLMForTouchpoint and LLMFingerprint to avoid duplication.
func resolveTouchpoint(cfg *EffectiveConfig, touchpoint string) (provider, model string) {
	if tp, ok := cfg.AITouchpoints[touchpoint]; ok && tp.Provider != "" {
		p := tp.Provider
		m := tp.Model
		if m == "" {
			switch p {
			case "ollama":
				m = cfg.OllamaLLMModel.Effective
			default:
				m = anthropicFallbackModel(cfg, touchpoint)
			}
		}
		return p, m
	}
	// Team-level fallback.
	if cfg.LLMProvider.Effective == "ollama" {
		return "ollama", cfg.OllamaLLMModel.Effective
	}
	return "anthropic", anthropicFallbackModel(cfg, touchpoint)
}

// wrapLogged wraps a resolved client with logging context, preserving untyped
// nil (a typed-nil inside the interface would defeat callers' nil checks).
func wrapLogged(c llm.Client, provider, model, touchpoint, teamID string) llm.Client {
	if c == nil {
		slog.Warn("llm unconfigured", "touchpoint", touchpoint, "team", teamID, "provider", provider, "model", model)
		return nil
	}
	return &llm.LoggingClient{Inner: c, Attrs: []any{"provider", provider, "model", model, "touchpoint", touchpoint, "team", teamID}}
}

// anthropicFallbackModel returns the Anthropic model used for a touchpoint when
// no explicit touchpoint model is configured.
func anthropicFallbackModel(cfg *EffectiveConfig, touchpoint string) string {
	switch touchpoint {
	case TouchpointAgents:
		return cfg.AgentModel.Effective
	case TouchpointImprovement:
		return improvementHaikuModel
	default: // analysis, enrichment
		return cfg.AnthropicModel.Effective
	}
}

// LLMForTouchpoint resolves the client for one AI touchpoint:
// explicit touchpoint entry → team default provider → env defaults.
// Returns nil when the resolved provider is unconfigured (callers skip+log).
func (s *Sources) LLMForTouchpoint(ctx context.Context, teamID, touchpoint string) llm.Client {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config", "touchpoint", touchpoint, "team", teamID, "err", err)
		return nil
	}
	provider, model := resolveTouchpoint(cfg, touchpoint)
	var raw llm.Client
	switch provider {
	case "ollama":
		raw = s.LLM.Ollama(cfg.OllamaURL.Effective, model)
	default: // anthropic or empty → Anthropic for backward compat
		raw = s.LLM.Client(cfg.AnthropicAPIKey.Effective, model)
	}
	return wrapLogged(raw, provider, model, touchpoint, teamID)
}

// AnalysisLLM returns a cached LLM client for the effective provider and model
// for teamID. Returns nil when no effective API key (Anthropic) or URL/model
// (Ollama) is configured. If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) AnalysisLLM(ctx context.Context, teamID string) llm.Client {
	return s.LLMForTouchpoint(ctx, teamID, TouchpointAnalysis)
}

// AgentLLM returns a cached LLM client for the effective provider and agent model
// for teamID. Returns nil when no effective API key (Anthropic) or URL/model
// (Ollama) is configured. If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) AgentLLM(ctx context.Context, teamID string) llm.Client {
	return s.LLMForTouchpoint(ctx, teamID, TouchpointAgents)
}

// ImprovementLLM returns a cached LLM client for the effective provider for
// teamID. When the provider is Anthropic, the model is pinned to
// claude-haiku-4-5-20251001 regardless of team settings; when the provider is
// Ollama, the team's configured chat model is used instead.
// Returns nil when no effective API key (Anthropic) or URL/model (Ollama) is
// configured. If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) ImprovementLLM(ctx context.Context, teamID string) llm.Client {
	return s.LLMForTouchpoint(ctx, teamID, TouchpointImprovement)
}

// EnrichmentLLM returns the client for prompt enrichment (enrich_context,
// prompt_suggest). See LLMForTouchpoint for resolution rules.
func (s *Sources) EnrichmentLLM(ctx context.Context, teamID string) llm.Client {
	return s.LLMForTouchpoint(ctx, teamID, TouchpointEnrichment)
}

// Embedder returns a cached Embedder for the effective Ollama URL+model for teamID.
// Returns nil when no effective Ollama URL is configured.
// If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) Embedder(ctx context.Context, teamID string) embedding.Embedder {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for Embedder", "team", teamID, "err", err)
		return nil
	}
	return s.Embed.Embedder(cfg.OllamaURL.Effective, cfg.OllamaModel.Effective)
}

// ChunkConfig returns the effective content-chunking configuration for teamID,
// resolved as saved team value (when > 0) over env default. On resolution error
// it returns a zero-value ChunkConfig (callers should treat that as "unset").
// If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) ChunkConfig(ctx context.Context, teamID string) embedding.ChunkConfig {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for ChunkConfig", "team", teamID, "err", err)
		return embedding.ChunkConfig{}
	}
	return embedding.ChunkConfig{
		MaxTokens:     cfg.EmbeddingMaxTokens,
		OverlapTokens: cfg.ChunkOverlapTokens,
		MaxChunks:     cfg.MaxChunks,
	}
}

// LLMFingerprint returns a stable identifier for the effective LLM
// provider+model used for teamID's touchpoint work, for cache
// discrimination. Returns "" when config cannot be resolved.
// The fingerprint never includes the API key — only provider+model.
func (s *Sources) LLMFingerprint(ctx context.Context, teamID, touchpoint string) string {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for LLMFingerprint", "team", teamID, "err", err)
		return ""
	}
	provider, model := resolveTouchpoint(cfg, touchpoint)
	switch provider {
	case "ollama":
		return "ollama|" + cfg.OllamaURL.Effective + "|" + model
	default:
		return "anthropic|" + model
	}
}
