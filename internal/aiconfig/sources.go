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

// clientFor returns the LLM client for cfg's effective provider. anthropicModel
// is the model used when the provider is Anthropic (each resolver role pins its
// own). Provider "ollama" uses the team's Ollama chat model; anything else
// (including empty) means Anthropic for backward compatibility.
func (s *Sources) clientFor(cfg *EffectiveConfig, anthropicModel string) llm.Client {
	if cfg.LLMProvider.Effective == "ollama" {
		return s.LLM.Ollama(cfg.OllamaURL.Effective, cfg.OllamaLLMModel.Effective)
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, anthropicModel)
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
	if tp, ok := cfg.AITouchpoints[touchpoint]; ok && tp.Provider != "" {
		switch tp.Provider {
		case "ollama":
			model := tp.Model
			if model == "" {
				model = cfg.OllamaLLMModel.Effective
			}
			return s.LLM.Ollama(cfg.OllamaURL.Effective, model)
		case "anthropic":
			model := tp.Model
			if model == "" {
				model = anthropicFallbackModel(cfg, touchpoint)
			}
			return s.LLM.Client(cfg.AnthropicAPIKey.Effective, model)
		}
	}
	return s.clientFor(cfg, anthropicFallbackModel(cfg, touchpoint))
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
	// Check explicit touchpoint override first.
	if tp, ok := cfg.AITouchpoints[touchpoint]; ok && tp.Provider != "" {
		switch tp.Provider {
		case "ollama":
			model := tp.Model
			if model == "" {
				model = cfg.OllamaLLMModel.Effective
			}
			return "ollama|" + cfg.OllamaURL.Effective + "|" + model
		case "anthropic":
			model := tp.Model
			if model == "" {
				model = anthropicFallbackModel(cfg, touchpoint)
			}
			return "anthropic|" + model
		}
	}
	// Fall back to team-level provider.
	if cfg.LLMProvider.Effective == "ollama" {
		return "ollama|" + cfg.OllamaURL.Effective + "|" + cfg.OllamaLLMModel.Effective
	}
	return "anthropic|" + anthropicFallbackModel(cfg, touchpoint)
}
