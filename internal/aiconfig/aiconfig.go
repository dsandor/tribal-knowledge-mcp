// Package aiconfig resolves effective AI configuration for a team by merging
// saved team settings with process-environment defaults.
//
// Masking of sensitive fields (e.g. AnthropicAPIKey) is intentionally NOT done
// here; that is the responsibility of the HTTP presentation layer so that
// internal callers always receive the full, usable value.
package aiconfig

import (
	"context"
	"errors"

	"github.com/dsandor/memory/internal/storage"
)

// FieldValue describes one effective configuration field and where it came from.
type FieldValue struct {
	Effective string `json:"effective"`
	Saved     string `json:"saved"`
	Env       string `json:"env"`
	// Source is one of: "saved" | "env" | "none"
	Source string `json:"source"`
}

// EffectiveConfig is the fully-resolved AI configuration for a team.
type EffectiveConfig struct {
	AnthropicAPIKey FieldValue `json:"anthropic_api_key"`
	AnthropicModel  FieldValue `json:"anthropic_model"`
	AgentModel      FieldValue `json:"agent_model"`
	OllamaURL       FieldValue `json:"ollama_url"`
	OllamaModel     FieldValue `json:"ollama_model"`
	LLMProvider     FieldValue `json:"llm_provider"`
	OllamaLLMModel  FieldValue `json:"ollama_llm_model"`
	// AITouchpoints maps touchpoint name to per-touchpoint AI config.
	// Valid keys: "analysis", "agents", "improvement", "enrichment".
	// No env layer — only saved team settings populate this map.
	AITouchpoints map[string]storage.AITouchpoint `json:"ai_touchpoints"`
}

// EnvDefaults carries the process-environment-derived defaults captured at
// startup. Callers populate this from os.Getenv (or equivalent) once and pass
// it to NewResolver.
type EnvDefaults struct {
	AnthropicAPIKey string
	AnthropicModel  string
	AgentModel      string
	OllamaURL       string
	OllamaModel     string
	LLMProvider     string
	OllamaLLMModel  string
}

// SettingsStore is the narrow storage interface required by the Resolver.
// *storage.SQLiteStore (and any TeamStore implementation) satisfies this.
type SettingsStore interface {
	GetTeamSettings(ctx context.Context, teamID string) (*storage.TeamSettings, error)
}

// Resolver resolves effective AI configuration for a given team by merging
// saved team settings with process-level environment defaults.
type Resolver struct {
	store SettingsStore
	env   EnvDefaults
}

// NewResolver creates a new Resolver backed by store and env.
func NewResolver(store SettingsStore, env EnvDefaults) *Resolver {
	return &Resolver{store: store, env: env}
}

// Effective returns the merged EffectiveConfig for teamID.
//
// If the store returns storage.ErrNotFound, or returns a nil settings pointer,
// all saved fields are treated as empty (not an error). Any other store error
// is propagated to the caller unchanged.
func (r *Resolver) Effective(ctx context.Context, teamID string) (*EffectiveConfig, error) {
	var saved storage.TeamSettings

	ts, err := r.store.GetTeamSettings(ctx, teamID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		// No settings row yet; treat as all-empty saved values.
	case err != nil:
		return nil, err
	case ts == nil:
		// Defensive: store returned nil without an error; treat as empty.
	default:
		saved = *ts
	}

	touchpoints := saved.AITouchpoints
	if touchpoints == nil {
		touchpoints = map[string]storage.AITouchpoint{}
	}

	cfg := &EffectiveConfig{
		AnthropicAPIKey: resolve(saved.AnthropicAPIKey, r.env.AnthropicAPIKey),
		AnthropicModel:  resolve(saved.AnthropicModel, r.env.AnthropicModel),
		AgentModel:      resolve(saved.AgentModel, r.env.AgentModel),
		OllamaURL:       resolve(saved.OllamaURL, r.env.OllamaURL),
		OllamaModel:     resolve(saved.OllamaModel, r.env.OllamaModel),
		LLMProvider:     resolve(saved.LLMProvider, r.env.LLMProvider),
		OllamaLLMModel:  resolve(saved.OllamaLLMModel, r.env.OllamaLLMModel),
		AITouchpoints:   touchpoints,
	}
	return cfg, nil
}

// resolve builds a FieldValue by merging a single saved value with the
// corresponding environment default.
//
//   - If saved is non-empty, Effective = saved, Source = "saved".
//   - Else if env is non-empty, Effective = env,   Source = "env".
//   - Otherwise Effective = "",  Source = "none".
func resolve(saved, env string) FieldValue {
	switch {
	case saved != "":
		return FieldValue{Effective: saved, Saved: saved, Env: env, Source: "saved"}
	case env != "":
		return FieldValue{Effective: env, Saved: "", Env: env, Source: "env"}
	default:
		return FieldValue{Effective: "", Saved: "", Env: "", Source: "none"}
	}
}
