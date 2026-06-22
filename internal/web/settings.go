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
		AnthropicAPIKey *string                         `json:"anthropic_api_key"`
		AnthropicModel  string                          `json:"anthropic_model"`
		OllamaURL       string                          `json:"ollama_url"`
		OllamaModel     string                          `json:"ollama_model"`
		LLMProvider     string                          `json:"llm_provider"`
		OllamaLLMModel  string                          `json:"ollama_llm_model"`
		AITouchpoints   map[string]storage.AITouchpoint `json:"ai_touchpoints"`
		// Per-team embedding/chunking config. 0 means "unset → env default".
		EmbeddingMaxTokens int `json:"embedding_max_tokens"`
		ChunkOverlapTokens int `json:"chunk_overlap_tokens"`
		MaxChunks          int `json:"max_chunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	// Validate embedding/chunking ints: reject negatives. 0 is allowed and means
	// "unset → fall back to the env default".
	if body.EmbeddingMaxTokens < 0 {
		writeError(w, 400, "bad_request", "embedding_max_tokens must be >= 0")
		return
	}
	if body.ChunkOverlapTokens < 0 {
		writeError(w, 400, "bad_request", "chunk_overlap_tokens must be >= 0")
		return
	}
	if body.MaxChunks < 0 {
		writeError(w, 400, "bad_request", "max_chunks must be >= 0")
		return
	}

	// Validate llm_provider: only empty string, "anthropic", or "ollama" are allowed.
	if body.LLMProvider != "" && body.LLMProvider != "anthropic" && body.LLMProvider != "ollama" {
		writeError(w, 400, "bad_request", "llm_provider must be anthropic or ollama")
		return
	}

	// Validate ai_touchpoints: only the four known keys and valid providers.
	validTouchpoints := map[string]bool{
		"analysis":    true,
		"agents":      true,
		"improvement": true,
		"enrichment":  true,
	}
	for k, tp := range body.AITouchpoints {
		if !validTouchpoints[k] {
			writeError(w, 400, "bad_request", fmt.Sprintf("unknown ai touchpoint %q", k))
			return
		}
		if tp.Provider != "" && tp.Provider != "anthropic" && tp.Provider != "ollama" {
			writeError(w, 400, "bad_request", fmt.Sprintf("ai touchpoint %q: provider must be anthropic or ollama", k))
			return
		}
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

	// Nil ai_touchpoints body field → store empty map (full-replace semantics:
	// the UI always sends the complete map; nil means no touchpoints configured).
	aiTouchpoints := body.AITouchpoints
	if aiTouchpoints == nil {
		aiTouchpoints = map[string]storage.AITouchpoint{}
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
		LLMProvider:        body.LLMProvider,
		OllamaLLMModel:     body.OllamaLLMModel,
		AITouchpoints:      aiTouchpoints,
		EmbeddingMaxTokens: body.EmbeddingMaxTokens,
		ChunkOverlapTokens: body.ChunkOverlapTokens,
		MaxChunks:          body.MaxChunks,
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
