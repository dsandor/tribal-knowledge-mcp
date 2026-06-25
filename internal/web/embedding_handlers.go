package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dsandor/memory/internal/embedding"
	"github.com/dsandor/memory/internal/storage"
)

// maskEmbeddingConfig builds the JSON response for the embedding config with the
// OpenAI API key masked: "stored" when a key is persisted, "" otherwise. This
// mirrors how team settings mask anthropic_api_key — the raw key never leaves
// the server. current_dimension is the persisted (live) column dimension;
// model_dimension is the configured model's known dimension (0 if unknown, in
// which case the UI shows "unknown" / probe-needed).
func maskEmbeddingConfig(cfg *storage.EmbeddingConfig) map[string]any {
	maskedKey := ""
	if cfg.OpenAIAPIKey != "" {
		maskedKey = "stored"
	}
	return map[string]any{
		"provider":          cfg.Provider,
		"model":             cfg.Model,
		"openai_api_key":    maskedKey,
		"openai_base_url":   cfg.OpenAIBaseURL,
		"ollama_url":        cfg.OllamaURL,
		"current_dimension": cfg.Dimension,
		"model_dimension":   embedding.ModelDimension(cfg.Model),
	}
}

// handleGetEmbeddingConfig returns the deployment embedding config with the
// OpenAI API key masked. Gated by RequireSuperadmin at the route level.
func (s *Server) handleGetEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetEmbeddingConfig(r.Context())
	if err != nil || cfg == nil {
		writeError(w, 500, "internal_error", "load embedding config")
		return
	}
	writeJSON(w, maskEmbeddingConfig(cfg))
}

// handlePutEmbeddingConfig updates the deployment embedding config. The OpenAI
// API key is preserved when the incoming value is empty or the literal "stored"
// (the masked sentinel the UI echoes back), so the stored key is never clobbered
// by a round-trip. The physical column Dimension is preserved — only the
// re-embed action changes it. Gated by RequireSuperadmin at the route level.
func (s *Server) handlePutEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		OpenAIAPIKey  string `json:"openai_api_key"`
		OpenAIBaseURL string `json:"openai_base_url"`
		OllamaURL     string `json:"ollama_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	existing, err := s.store.GetEmbeddingConfig(ctx)
	if err != nil || existing == nil {
		writeError(w, 500, "internal_error", "load embedding config")
		return
	}

	// Preserve the stored key when the caller sends nothing meaningful (empty or
	// the masked "stored" sentinel); otherwise adopt the new key.
	apiKey := existing.OpenAIAPIKey
	if body.OpenAIAPIKey != "" && body.OpenAIAPIKey != "stored" {
		apiKey = body.OpenAIAPIKey
	}

	updated := storage.EmbeddingConfig{
		Provider:      body.Provider,
		Model:         body.Model,
		OpenAIAPIKey:  apiKey,
		OpenAIBaseURL: body.OpenAIBaseURL,
		OllamaURL:     body.OllamaURL,
		Dimension:     existing.Dimension, // only re-embed changes the column dimension
	}
	if err := s.store.PutEmbeddingConfig(ctx, updated); err != nil {
		writeError(w, 500, "internal_error", "persist embedding config")
		return
	}

	writeJSON(w, maskEmbeddingConfig(&updated))
}

// reembedPageSize is the page size used to enumerate all entries during a
// re-embed-all run. ListEntries with an empty filter applies an implementation
// default limit, so the handler pages explicitly to avoid silently capping the
// set of re-embedded entries.
const reembedPageSize = 500

// handleReembedAll re-embeds every entry across all teams using the currently
// configured embedder. It is gated by RequireSuperadmin at the route level.
//
// Flow:
//  1. Load the deployment embedding config and resolve the active embedder.
//  2. Determine the target vector dimension: ModelDimension(cfg.Model) for
//     providers with a known static dimension (OpenAI); otherwise embed a probe
//     text and use len(vector) (e.g. Ollama, where the dimension is model- and
//     server-dependent).
//  3. If the target dimension differs from the configured one, rebuild the
//     vector columns at the new dimension and persist the updated config.
//  4. Enumerate all entries (paging past the list default limit), chunk + embed
//     each, and replace its stored chunks. Per-entry failures are logged and
//     counted as skipped without aborting the whole job.
func (s *Server) handleReembedAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if s.aiSrc == nil {
		writeError(w, 400, "not_configured", "embedding not configured")
		return
	}

	cfg, err := s.store.GetEmbeddingConfig(ctx)
	if err != nil || cfg == nil {
		writeError(w, 500, "internal_error", "load embedding config")
		return
	}

	embedder := s.aiSrc.Embedder(ctx, "")
	if embedder == nil {
		writeError(w, 400, "not_configured", "embedding not configured")
		return
	}

	// Resolve the target dimension. Known models (OpenAI) report it statically;
	// otherwise probe the embedder to discover it.
	dim := embedding.ModelDimension(cfg.Model)
	if dim == 0 {
		if cfg.Provider == "openai" {
			// OpenAI models should have a known dimension; an unknown one is a
			// configuration error rather than something to probe for.
			writeError(w, 400, "bad_request", "unknown embedding model dimension")
			return
		}
		v, err := embedder.Embed(ctx, "dimension probe")
		if err != nil || len(v) == 0 {
			writeError(w, 400, "bad_request", "could not determine embedding dimension")
			return
		}
		dim = len(v)
	}

	// If the physical column dimension no longer matches, rebuild the vector
	// columns and persist the new dimension before re-embedding.
	if dim != cfg.Dimension {
		if err := s.store.RebuildEmbeddingColumns(ctx, dim); err != nil {
			writeError(w, 500, "internal_error", "rebuild embedding columns")
			return
		}
		updated := *cfg
		updated.Dimension = dim
		if err := s.store.PutEmbeddingConfig(ctx, updated); err != nil {
			writeError(w, 500, "internal_error", "persist embedding config")
			return
		}
	}

	var (
		reembedded int
		skipped    int
		offset     int
	)
	for {
		entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: reembedPageSize, Offset: offset})
		if err != nil {
			writeError(w, 500, "internal_error", "list entries")
			return
		}
		if len(entries) == 0 {
			break
		}
		for i := range entries {
			entry := entries[i]
			if s.reembedEntry(ctx, embedder, entry) {
				reembedded++
			} else {
				skipped++
			}
		}
		if len(entries) < reembedPageSize {
			break
		}
		offset += len(entries)
	}

	writeJSON(w, map[string]any{
		"reembedded": reembedded,
		"skipped":    skipped,
		"dimension":  dim,
	})
}

// reembedEntry chunks, embeds, and replaces the stored chunks for a single
// entry. It returns true on success; on any failure it logs a warning and
// returns false so the caller can count it as skipped without aborting the run.
func (s *Server) reembedEntry(ctx context.Context, embedder embedding.Embedder, entry storage.KnowledgeEntry) bool {
	cfg := s.aiSrc.ChunkConfig(ctx, entry.TeamID)
	chunks := embedding.Chunk(entry.Content, cfg)
	entryChunks := make([]storage.EntryChunk, 0, len(chunks))
	for _, c := range chunks {
		emb, err := embedder.Embed(ctx, c.Content)
		if err != nil {
			slog.Warn("reembed: embedding failed", "id", entry.ID, "err", err)
			return false
		}
		entryChunks = append(entryChunks, storage.EntryChunk{
			Index:         c.Index,
			Content:       c.Content,
			TokenEstimate: c.TokenEstimate,
			Embedding:     emb,
		})
	}
	if err := s.store.ReplaceEntryChunks(ctx, entry.ID, entryChunks); err != nil {
		slog.Warn("reembed: replace chunks failed", "id", entry.ID, "err", err)
		return false
	}
	return true
}
