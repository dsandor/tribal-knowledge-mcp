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
}

// EmbedProvider is the narrow interface required by Sources to obtain cached Embedders.
// *embedding.Provider satisfies this.
type EmbedProvider interface {
	Embedder(url, model string) embedding.Embedder
}

// Sources resolves ready-to-use AI clients from the effective configuration on
// each call, so saved team settings take effect immediately without a restart.
type Sources struct {
	Resolver    *Resolver
	LLM         LLMProvider
	Embed       EmbedProvider
	// DefaultTeam is the team ID applied as a fallback by every method
	// (AnalysisLLM, AgentLLM, ImprovementLLM, Embedder) when the caller
	// supplies an empty teamID. This is typically set at startup to the
	// single-team value derived from the environment (e.g. stdio MCP
	// connections that carry no team context in the request).
	DefaultTeam string
}

// AnalysisLLM returns a cached LLM client for the effective Anthropic key+model
// for teamID. Returns nil when no effective API key is configured.
// If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) AnalysisLLM(ctx context.Context, teamID string) llm.Client {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for AnalysisLLM", "team", teamID, "err", err)
		return nil
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, cfg.AnthropicModel.Effective)
}

// AgentLLM returns a cached LLM client for the effective Anthropic key + agent model
// for teamID. Returns nil when no effective API key is configured.
// If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) AgentLLM(ctx context.Context, teamID string) llm.Client {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for AgentLLM", "team", teamID, "err", err)
		return nil
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, cfg.AgentModel.Effective)
}

// ImprovementLLM returns a cached LLM client for the effective Anthropic key +
// the pinned improvement model (claude-haiku-4-5-20251001).
// Returns nil when no effective API key is configured.
// If teamID is empty, DefaultTeam is used as a fallback.
func (s *Sources) ImprovementLLM(ctx context.Context, teamID string) llm.Client {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config for ImprovementLLM", "team", teamID, "err", err)
		return nil
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, "claude-haiku-4-5-20251001")
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
