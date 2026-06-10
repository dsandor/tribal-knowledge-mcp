package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	s.handleGetSettingsEnriched(w, r)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	var body struct {
		Domains            []string `json:"domains"`
		ClusterThreshold   float64  `json:"cluster_threshold"`
		PipelineMinEntries int      `json:"pipeline_min_entries"`
		AgentModel         string   `json:"agent_model"`
		// AnthropicAPIKey is a pointer so we can distinguish "not sent" (nil)
		// from "explicitly cleared" (pointer to ""). When nil, the existing key
		// stored in the DB is preserved.
		AnthropicAPIKey *string `json:"anthropic_api_key"`
		AnthropicModel  string  `json:"anthropic_model"`
		OllamaURL       string  `json:"ollama_url"`
		OllamaModel     string  `json:"ollama_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	// Read existing settings so we can preserve the stored API key when the
	// caller omits the field (e.g. the UI doesn't re-send the masked value).
	existing, err := s.store.GetTeamSettings(r.Context(), tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get settings: %v", err))
		return
	}

	apiKey := existing.AnthropicAPIKey
	if body.AnthropicAPIKey != nil {
		apiKey = *body.AnthropicAPIKey
	}

	settings := storage.TeamSettings{
		TeamID:             tc.TeamID,
		Domains:            body.Domains,
		ClusterThreshold:   body.ClusterThreshold,
		PipelineMinEntries: body.PipelineMinEntries,
		AgentModel:         body.AgentModel,
		AnthropicAPIKey:    apiKey,
		AnthropicModel:     body.AnthropicModel,
		OllamaURL:          body.OllamaURL,
		OllamaModel:        body.OllamaModel,
	}
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
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleGetAuthConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAuthConfig(r.Context())
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get auth config: %v", err))
		return
	}
	writeJSON(w, map[string]any{
		"provider":          cfg.Provider,
		"oidc_issuer":       cfg.OIDCIssuer,
		"oidc_client_id":    cfg.OIDCClientID,
		"oidc_redirect_url": cfg.OIDCRedirectURL,
		"updated_at":        cfg.UpdatedAt,
	})
}

func (s *Server) handlePutAuthConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider        string `json:"provider"`
		OIDCIssuer      string `json:"oidc_issuer"`
		OIDCClientID    string `json:"oidc_client_id"`
		OIDCRedirectURL string `json:"oidc_redirect_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	if body.Provider != "local" && body.Provider != "oidc" {
		writeError(w, 400, "bad_request", "provider must be 'local' or 'oidc'")
		return
	}
	if err := s.store.PutAuthConfig(r.Context(), storage.AuthConfig{
		Provider:        body.Provider,
		OIDCIssuer:      body.OIDCIssuer,
		OIDCClientID:    body.OIDCClientID,
		OIDCRedirectURL: body.OIDCRedirectURL,
	}); err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("put auth config: %v", err))
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
