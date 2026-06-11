package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/aiconfig"
	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

// AnthropicModelsURL is the Anthropic models list endpoint. Exported so tests
// can point it at a local httptest server without calling the real API.
var AnthropicModelsURL = "https://api.anthropic.com/v1/models"

// anthropicFallbackModels is the curated list returned when no API key is
// configured or the upstream call fails.
var anthropicFallbackModels = []modelEntry{
	{ID: "claude-fable-5", Label: "Fable 5"},
	{ID: "claude-opus-4-8", Label: "Opus 4.8"},
	{ID: "claude-sonnet-4-6", Label: "Sonnet 4.6"},
	{ID: "claude-haiku-4-5-20251001", Label: "Haiku 4.5"},
}

type modelEntry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// aiFieldValue is the masked/unmasked representation of one FieldValue for API responses.
type aiFieldValue struct {
	Effective string `json:"effective"`
	Saved     string `json:"saved"`
	Env       string `json:"env"`
	Source    string `json:"source"`
}

// maskKeyFieldValue masks an API-key FieldValue so no raw key material is
// returned to the client. Non-empty values for Effective/Saved/Env are
// replaced with the literal "stored"; Source passes through unchanged.
func maskKeyFieldValue(fv aiconfig.FieldValue) aiFieldValue {
	mask := func(s string) string {
		if s != "" {
			return "stored"
		}
		return ""
	}
	return aiFieldValue{
		Effective: mask(fv.Effective),
		Saved:     mask(fv.Saved),
		Env:       mask(fv.Env),
		Source:    fv.Source,
	}
}

func plainFieldValue(fv aiconfig.FieldValue) aiFieldValue {
	return aiFieldValue{
		Effective: fv.Effective,
		Saved:     fv.Saved,
		Env:       fv.Env,
		Source:    fv.Source,
	}
}

// resolveEffective resolves the effective config for the team extracted from
// the request context. When aiSrc is nil it returns nil, nil.
func (s *Server) resolveEffective(ctx context.Context, teamID string) (*aiconfig.EffectiveConfig, error) {
	if s.aiSrc == nil {
		return nil, nil
	}
	if teamID == "" {
		teamID = s.aiSrc.DefaultTeam
	}
	return s.aiSrc.Resolver.Effective(ctx, teamID)
}

// buildAIBlock builds the "ai" block for the GET /api/settings response.
// It returns nil when aiSrc is not configured.
func (s *Server) buildAIBlock(ctx context.Context, teamID string) map[string]any {
	eff, err := s.resolveEffective(ctx, teamID)
	if err != nil || eff == nil {
		return nil
	}
	return map[string]any{
		"anthropic_api_key": maskKeyFieldValue(eff.AnthropicAPIKey),
		"anthropic_model":   plainFieldValue(eff.AnthropicModel),
		"agent_model":       plainFieldValue(eff.AgentModel),
		"ollama_url":        plainFieldValue(eff.OllamaURL),
		"ollama_model":      plainFieldValue(eff.OllamaModel),
		"llm_provider":      plainFieldValue(eff.LLMProvider),
		"ollama_llm_model":  plainFieldValue(eff.OllamaLLMModel),
		// ai_touchpoints is a plain map — no FieldValue wrapper, no env layer.
		"ai_touchpoints": eff.AITouchpoints,
	}
}

// buildSettingsResponse builds the map that GET /api/settings sends back. It replicates
// the exact JSON keys that the current writeJSON(w, settings) call produces
// (mix of tagged snake_case and untagged PascalCase from storage.TeamSettings)
// and adds an "ai" key when aiBlock is non-nil.
//
// anthropic_api_key is intentionally excluded from this function: the raw key
// must never enter the response map even transiently. Callers are responsible
// for setting the masked value explicitly via resp["anthropic_api_key"] = maskedKey
// after calling this function.
func buildSettingsResponse(settings *storage.TeamSettings, aiBlock map[string]any) map[string]any {
	// Replicate the CURRENT wire format.  Tagged fields keep their json tag
	// names; untagged fields marshal as their Go identifier (PascalCase).
	//
	// ai_touchpoints is included as a plain map (no FieldValue wrapper — there is
	// no env layer for touchpoints) so the UI can hydrate the picker state.
	touchpoints := settings.AITouchpoints
	if touchpoints == nil {
		touchpoints = map[string]storage.AITouchpoint{}
	}
	m := map[string]any{
		// Untagged fields — marshalled by encoding/json as their Go name.
		"TeamID":             settings.TeamID,
		"Domains":            settings.Domains,
		"ClusterThreshold":   settings.ClusterThreshold,
		"PipelineMinEntries": settings.PipelineMinEntries,
		"agent_model":        settings.AgentModel,
		"UpdatedAt":          settings.UpdatedAt,
		// Tagged fields — use the json struct tag value.
		// anthropic_api_key is omitted here; callers must set the masked value explicitly.
		"anthropic_model":  settings.AnthropicModel,
		"ollama_url":       settings.OllamaURL,
		"ollama_model":     settings.OllamaModel,
		"llm_provider":     settings.LLMProvider,
		"ollama_llm_model": settings.OllamaLLMModel,
		// ai_touchpoints: plain map for UI hydration (no env layer).
		"ai_touchpoints": touchpoints,
	}
	if aiBlock != nil {
		m["ai"] = aiBlock
	}
	return m
}

// handleGetSettingsEnriched replaces the plain writeJSON call in handleGetSettings
// with one that adds the "ai" block.
func (s *Server) handleGetSettingsEnriched(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	settings, err := s.store.GetTeamSettings(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get settings: %v", err))
		return
	}

	// Mask the API key — the frontend only needs to know whether one is stored.
	maskedKey := settings.AnthropicAPIKey
	if maskedKey != "" {
		maskedKey = "stored"
	}

	// Build the "ai" block (nil when aiSrc is not configured).
	aiBlock := s.buildAIBlock(r.Context(), tc.TeamID)

	// Build response preserving the existing wire format.
	resp := buildSettingsResponse(settings, aiBlock)
	resp["anthropic_api_key"] = maskedKey // set masked value (raw key never enters the map)

	writeJSON(w, resp)
}

// handleGetModels handles GET /api/settings/models.
// It returns available Anthropic and Ollama models by probing upstream APIs.
// The endpoint never returns 5xx for upstream failures; degraded results are
// returned instead. It returns 503 only when aiSrc is not configured.
func (s *Server) handleGetModels(w http.ResponseWriter, r *http.Request) {
	if s.aiSrc == nil {
		writeError(w, 503, "not_configured", "AI sources not configured")
		return
	}

	tc := auth.GetTeamContext(r.Context())
	eff, err := s.resolveEffective(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("resolve config: %v", err))
		return
	}

	// --- Anthropic models ---
	anthropicModels, anthropicSource := fetchAnthropicModels(r.Context(), eff.AnthropicAPIKey.Effective)

	// --- Ollama models ---
	ollamaModels, ollamaSource := fetchOllamaModels(r.Context(), eff.OllamaURL.Effective)

	writeJSON(w, map[string]any{
		"anthropic":        anthropicModels,
		"ollama":           ollamaModels,
		"anthropic_source": anthropicSource,
		"ollama_source":    ollamaSource,
	})
}

// fetchAnthropicModels fetches the list of available Anthropic models using the
// provided API key. Returns the fallback curated list when the key is empty or
// the upstream call fails. ctx is the caller's request context; a 5-second
// timeout is derived from it so the upstream call is also cancelled when the
// request itself is cancelled.
func fetchAnthropicModels(ctx context.Context, apiKey string) ([]modelEntry, string) {
	if apiKey == "" {
		return anthropicFallbackModels, "fallback"
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AnthropicModelsURL, nil)
	if err != nil {
		return anthropicFallbackModels, "fallback"
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return anthropicFallbackModels, "fallback"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return anthropicFallbackModels, "fallback"
	}

	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return anthropicFallbackModels, "fallback"
	}

	models := make([]modelEntry, 0, len(body.Data))
	for _, m := range body.Data {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		models = append(models, modelEntry{ID: m.ID, Label: label})
	}
	if len(models) == 0 {
		return anthropicFallbackModels, "fallback"
	}
	return models, "api"
}

// fetchOllamaModels fetches the list of available models from the Ollama server
// at the given URL. Returns an empty list with source "unavailable" on any error.
// ctx is the caller's request context; a 2-second timeout is derived from it so
// the upstream call is also cancelled when the request itself is cancelled.
func fetchOllamaModels(ctx context.Context, ollamaURL string) ([]modelEntry, string) {
	if ollamaURL == "" {
		return []modelEntry{}, "unavailable"
	}

	tagsURL := strings.TrimRight(ollamaURL, "/") + "/api/tags"

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return []modelEntry{}, "unavailable"
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return []modelEntry{}, "unavailable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []modelEntry{}, "unavailable"
	}

	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return []modelEntry{}, "unavailable"
	}

	models := make([]modelEntry, 0, len(body.Models))
	for _, m := range body.Models {
		models = append(models, modelEntry{ID: m.Name, Label: m.Name})
	}
	return models, "api"
}

// handleImportEnv handles POST /api/settings/import-env.
// It copies non-empty ENV values for the requested fields into the team's saved
// settings, then responds with the refreshed masked "ai" block.
func (s *Server) handleImportEnv(w http.ResponseWriter, r *http.Request) {
	if s.aiSrc == nil {
		writeError(w, 503, "not_configured", "AI sources not configured")
		return
	}

	tc := auth.GetTeamContext(r.Context())

	var body struct {
		Fields []string `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	// Validate all requested field names up-front.
	validFields := map[string]bool{
		"anthropic_api_key": true,
		"anthropic_model":   true,
		"agent_model":       true,
		"ollama_url":        true,
		"ollama_model":      true,
		"llm_provider":      true,
		"ollama_llm_model":  true,
	}
	for _, f := range body.Fields {
		if !validFields[f] {
			writeError(w, 400, "bad_request", fmt.Sprintf("unknown field: %q", f))
			return
		}
	}

	// Resolve effective config to get ENV values.
	teamID := tc.TeamID
	if teamID == "" {
		teamID = s.aiSrc.DefaultTeam
	}
	eff, err := s.aiSrc.Resolver.Effective(r.Context(), teamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("resolve config: %v", err))
		return
	}

	// Read-modify-write: preserve all existing saved fields.
	existing, err := s.store.GetTeamSettings(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get settings: %v", err))
		return
	}

	settings := *existing

	// Apply each requested field if its ENV value is non-empty.
	for _, f := range body.Fields {
		switch f {
		case "anthropic_api_key":
			if eff.AnthropicAPIKey.Env != "" {
				settings.AnthropicAPIKey = eff.AnthropicAPIKey.Env
			}
		case "anthropic_model":
			if eff.AnthropicModel.Env != "" {
				settings.AnthropicModel = eff.AnthropicModel.Env
			}
		case "agent_model":
			if eff.AgentModel.Env != "" {
				settings.AgentModel = eff.AgentModel.Env
			}
		case "ollama_url":
			if eff.OllamaURL.Env != "" {
				settings.OllamaURL = eff.OllamaURL.Env
			}
		case "ollama_model":
			if eff.OllamaModel.Env != "" {
				settings.OllamaModel = eff.OllamaModel.Env
			}
		case "llm_provider":
			if eff.LLMProvider.Env != "" {
				settings.LLMProvider = eff.LLMProvider.Env
			}
		case "ollama_llm_model":
			if eff.OllamaLLMModel.Env != "" {
				settings.OllamaLLMModel = eff.OllamaLLMModel.Env
			}
		}
	}

	// Apply the same defaults as handlePutSettings. This is intentional: the
	// write path calls PutTeamSettings with ALL fields, so any field that is
	// zero-valued in the struct would be persisted as zero if we skip this
	// block. The defaults here mirror handlePutSettings exactly so that both
	// write paths (full settings PUT and AI-only import-env) produce consistent
	// results and never accidentally overwrite non-AI fields with zero values.
	if settings.ClusterThreshold == 0 {
		settings.ClusterThreshold = 0.85
	}
	if settings.PipelineMinEntries == 0 {
		settings.PipelineMinEntries = 10
	}
	if settings.AgentModel == "" {
		settings.AgentModel = "claude-haiku-4-5-20251001"
	}

	if err := s.store.PutTeamSettings(r.Context(), settings); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("put settings: %v", err))
		return
	}

	// Respond with the refreshed masked "ai" block.
	// Re-resolve to reflect the newly saved values.
	newEff, err := s.aiSrc.Resolver.Effective(r.Context(), teamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("resolve config after save: %v", err))
		return
	}
	aiBlock := map[string]any{
		"anthropic_api_key": maskKeyFieldValue(newEff.AnthropicAPIKey),
		"anthropic_model":   plainFieldValue(newEff.AnthropicModel),
		"agent_model":       plainFieldValue(newEff.AgentModel),
		"ollama_url":        plainFieldValue(newEff.OllamaURL),
		"ollama_model":      plainFieldValue(newEff.OllamaModel),
		"llm_provider":      plainFieldValue(newEff.LLMProvider),
		"ollama_llm_model":  plainFieldValue(newEff.OllamaLLMModel),
		"ai_touchpoints":    newEff.AITouchpoints,
	}
	writeJSON(w, map[string]any{"ai": aiBlock})
}
