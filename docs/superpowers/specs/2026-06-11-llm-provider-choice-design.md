# Selectable LLM Provider (Anthropic vs Ollama) — Design

**Date:** 2026-06-11
**Status:** Approved

## Problem

All LLM work (analysis clustering/scoring/gaps, agent generation, weak-signal improvement,
auto-tagging, enrich_context prompt improvement, prompt_suggest, agent refactor) is hard-wired
to the Anthropic Messages API via `llm.AnthropicClient`. Ollama is used only for embeddings.
Teams need to choose Anthropic or Ollama for LLM work — e.g. to run fully local.

## Decisions (user-approved)

| Decision | Choice |
|----------|--------|
| Granularity | One per-team switch (`llm_provider`) for all three resolver roles |
| Failure mode | Ollama unconfigured/unreachable ⇒ task fails/skips with logged warning — never silently falls back to Anthropic |
| Dispatch point | Inside `aiconfig.Sources`; zero call-site changes |

## Architecture

The seam is the existing one-method `llm.Client` interface (`Complete(ctx, prompt)`).
Every consumer already resolves clients through `aiconfig.Sources.{AnalysisLLM, AgentLLM,
ImprovementLLM}`, which re-resolve per call so saved settings apply immediately. Adding an
Ollama implementation behind that interface and a provider switch inside `Sources` makes
every AI feature provider-aware at once.

## Components

### 1. `internal/llm/ollama.go` — OllamaClient

```go
// OllamaClient calls a local Ollama server's generate API.
type OllamaClient struct { url, model string; client *http.Client; retryDelay func(int) time.Duration }

func NewOllamaClient(url, model string) *OllamaClient // 120s timeout (local models are slow)
func (c *OllamaClient) Complete(ctx context.Context, prompt string) (string, error)
```

- `POST {url}/api/generate` body `{"model": m, "prompt": p, "stream": false}` → parse
  `{"response": "..."}`; non-2xx or `{"error": "..."}` → error.
- Retry on 5xx/network errors with the same exponential-backoff shape as `AnthropicClient`
  (max 3 attempts).
- Unit-tested via `httptest` exactly like `anthropic_test.go`.

### 2. `internal/llm/provider.go` — cached Ollama factory

```go
// Ollama returns a cached OllamaClient for (url, model).
// Returns nil when url or model is empty (Ollama LLM not configured).
func (p *Provider) Ollama(url, model string) Client
```

Cache key `"ollama|" + url + "|" + model` (existing Anthropic entries keep their key shape).

### 3. Config plumbing — two new fields following the existing five

| Layer | Addition |
|-------|----------|
| `storage.TeamSettings` + both adapters' `team_settings` (idempotent ALTER) | `llm_provider TEXT NOT NULL DEFAULT ''`, `ollama_llm_model TEXT NOT NULL DEFAULT ''` (Get/Put queries updated) |
| `aiconfig.EffectiveConfig` | `LLMProvider`, `OllamaLLMModel` FieldValues (saved/env/effective merge via existing `resolve`) |
| `aiconfig.EnvDefaults` + `internal/config` + `cmd/server/main.go` | env vars `LLM_PROVIDER`, `OLLAMA_LLM_MODEL` |
| `.env.example` | documented defaults |

`ollama_llm_model` is the **chat** model (e.g. `llama3.1`); the existing `ollama_model`
remains the embedding model. Empty/unset `llm_provider` means `anthropic` (back-compat).

### 4. `aiconfig.Sources` — provider dispatch

```go
// LLMProvider interface gains: Ollama(url, model string) llm.Client

func (s *Sources) clientFor(cfg *EffectiveConfig, anthropicModel string) llm.Client {
	if cfg.LLMProvider.Effective == "ollama" {
		return s.LLM.Ollama(cfg.OllamaURL.Effective, cfg.OllamaLLMModel.Effective)
	}
	return s.LLM.Client(cfg.AnthropicAPIKey.Effective, anthropicModel)
}
```

- `AnalysisLLM` → `clientFor(cfg, cfg.AnthropicModel.Effective)`
- `AgentLLM` → `clientFor(cfg, cfg.AgentModel.Effective)`
- `ImprovementLLM` → Anthropic side stays pinned to `claude-haiku-4-5-20251001`; Ollama side
  uses the team's chat model (no per-role Ollama models — YAGNI).

Nil-client contract preserved: unconfigured provider ⇒ nil ⇒ each consumer's existing
skip-and-log path. Runtime Ollama errors fail only the task that hit them.

### 5. Settings surface

- `GET/PUT /api/settings` AI section + `POST /api/settings/import-env`: add `llm_provider`
  and `ollama_llm_model` as FieldValue-shaped entries, mirroring the existing five.
  PUT validates `llm_provider ∈ {"", "anthropic", "ollama"}` → 400 otherwise.
- React Settings page: an "LLM Provider" select (Anthropic / Ollama). When Ollama is
  selected, show an "Ollama chat model" picker fed by the existing
  `fetchModelOptions().ollama` list (free-text fallback preserved, matching the page's
  current model-field UX). `AISettings` interface in `api.ts` gains both fields.

## Non-Goals

- Embeddings stay Ollama-only (unchanged).
- No per-role provider/model matrix.
- No automatic Anthropic fallback.
- No new providers beyond these two (the seam makes additions cheap later).

## Testing

- `internal/llm`: OllamaClient success/error/retry via httptest; Provider.Ollama caching +
  nil contract.
- `internal/aiconfig`: resolver merge for new fields; Sources dispatch table tests
  (default→anthropic, saved ollama→ollama client, ollama selected but unconfigured→nil,
  ImprovementLLM model pinning per provider).
- `internal/web`: settings GET/PUT round-trip with new fields; invalid provider → 400.
- Storage: TeamSettings round-trip with new columns (both adapters compile; SQLite tested).
- `go test ./...` + clean Vite build.
