# Effective AI Configuration — Design Spec

**Date:** 2026-06-10
**Status:** Approved design, pending implementation
**Topic:** Make the Settings → AI Configuration panel show and control the *effective* AI config (saved config wins, env seeds it, live apply, model dropdowns)

## Problem

The AI settings panel edits `team_settings` rows (anthropic key/model, ollama url/model, agent model), but the runtime never consumes them: the Anthropic/Ollama clients are built **once at startup from environment variables** in `cmd/server/main.go`. So the panel neither shows the effective values nor affects behavior. Model fields are free-text. There is no way to see env-derived values or import them into the saved config.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Precedence | **Saved config wins; env is the fallback/seed.** Per-field: saved value if non-empty, else env. |
| Apply timing | **Live, no restart.** Effective config resolved per call/run; clients cached by resolved values. |
| Model lists | **Live:** Ollama `GET {url}/api/tags`; Anthropic `GET /v1/models` when a key is effective; curated Claude fallback list otherwise. |
| Env import | Per-field "Import from env" copies the env value into saved `team_settings`. |

## Architecture

### 1. `internal/aiconfig` — effective-config resolver (new package)

```go
type FieldValue struct {
    Effective string `json:"effective"`
    Saved     string `json:"saved"`
    Env       string `json:"env"`
    Source    string `json:"source"` // "saved" | "env" | "none"
}

type EffectiveConfig struct {
    AnthropicAPIKey FieldValue // values masked at the HTTP layer, not here
    AnthropicModel  FieldValue
    AgentModel      FieldValue
    OllamaURL       FieldValue
    OllamaModel     FieldValue
}

type EnvDefaults struct { // captured from config.Config at startup
    AnthropicAPIKey, AnthropicModel, AgentModel, OllamaURL, OllamaModel string
}

type Resolver struct { store storage.TeamStore; env EnvDefaults }
func NewResolver(store storage.TeamStore, env EnvDefaults) *Resolver
func (r *Resolver) Effective(ctx context.Context, teamID string) (*EffectiveConfig, error)
```

- Per field: `Saved` from `GetTeamSettings(teamID)` (empty TeamSettings on not-found is fine), `Env` from `EnvDefaults`, `Effective` = saved if non-empty else env, `Source` accordingly ("none" when both empty).
- No caching in the resolver itself (a `GetTeamSettings` read per resolution is acceptable; it backs interactive calls and pipeline runs, not hot loops).

### 2. Live client providers (cached, keyed by resolved values)

- `internal/llm`: `type Provider struct{...}` with `Client(apiKey, model string) Client` — returns nil when `apiKey == ""`; caches `*AnthropicClient` in a mutex-guarded map keyed `apiKey+"|"+model`.
- `internal/embedding`: `type Provider struct{...}` with `Embedder(url, model string) Embedder` — returns nil when `url == ""`; caches keyed `url+"|"+model`.
- No invalidation needed: a settings change produces a different key → new client. Old entries are tiny (HTTP clients); unbounded growth is bounded in practice by distinct config values; acceptable.

### 3. Consumers resolve per call/run

Wire `*aiconfig.Resolver` + providers into the places that currently capture a startup client:

- **`internal/mcp/enrich_context.go` / `prompt_suggest.go`**: instead of an `llm.Client` parameter, accept a `ClientSource` (small interface `func(ctx) llm.Client` or resolver+provider pair); resolve effective anthropic key/model per call (team from `auth.GetTeamContext`, fallback to the default `TEAM_ID` for stdio).
- **Embedding** (`knowledge store/search`, MCP tools): same pattern for the embedder (effective ollama url/model).
- **`internal/pipeline`**: resolve at the start of each run for the pipeline's configured team (analysis model = effective anthropic model; agent generation = effective agent model; weak-signal improvement keeps its pinned haiku model but uses the effective key).
- **Startup (`cmd/server/main.go`)**: build `EnvDefaults` from `cfg`, construct resolver + providers, pass them through. The `if cfg.AnthropicAPIKey != ""` gating for pipeline start changes to: start the pipeline if an anthropic key is effective for the default team at startup OR keep current behavior plus per-run re-check (keep simple: pipeline always constructed; each run no-ops with a logged skip when no effective key).
- Existing constructors (`NewAnthropicClient`, `NewOllamaEmbedder`) stay; providers wrap them.

### 4. Settings API (`internal/web/settings.go` + `server.go` routes)

- **`GET /api/settings`** — response gains `"ai": { anthropic_api_key, anthropic_model, agent_model, ollama_url, ollama_model }` where each is a `FieldValue`. Masking: for the key field, `effective`/`saved`/`env` are replaced with `"stored"`/`""` indicators (booleans semantics preserved via non-empty marker), never raw key material.
- **`GET /api/settings/models`** — `{ "anthropic": [{id,label}...], "ollama": [{id,label}...], "anthropic_source": "api"|"fallback", "ollama_source": "api"|"unavailable" }`. Ollama: `GET {effective ollama_url}/api/tags` (2s timeout), map `models[].name`. Anthropic: when an effective key exists, `GET https://api.anthropic.com/v1/models` (anthropic-version header, 5s timeout), map `data[].id`; else curated fallback: `claude-fable-5`, `claude-opus-4-8`, `claude-sonnet-4-6`, `claude-haiku-4-5-20251001`. Errors → fallback/empty, never 5xx.
- **`POST /api/settings/import-env`** — body `{ "fields": ["anthropic_api_key", ...] }`; for each named field with a non-empty env value, copy env → saved `team_settings`; respond with the refreshed `ai` block. Admin-gated like `PUT /api/settings`.

### 5. Frontend (`web/src/pages/Settings.tsx` + `web/src/lib/api.ts`)

- Types for `FieldValue`/`AISettings`/`ModelOption`; API fns `fetchModelOptions()`, `importEnvSettings(fields)`.
- Each AI field shows: the editable **saved** value, the **effective** value with a source chip (`saved` = green, `from env` = blue, `not set` = grey), and an **Import from env** button when env has a value and saved is empty/different.
- `anthropic_model`, `agent_model` → MUI `Autocomplete` (freeSolo) over anthropic options; `ollama_model` → Autocomplete over ollama options; both load from `/api/settings/models` on mount and re-fetch after saving the key/url. Free-text always allowed (freeSolo) so an unavailable Ollama or missing key never blocks input.
- Key field unchanged in input behavior (write-only, masked), but now displays whether an env key exists and offers import.

### 6. Storage

No schema change — `team_settings` already has all columns.

## Testing

- `aiconfig`: precedence (saved over env, env fallback, none), per-field sources.
- Providers: same key→same client instance; different key→different; empty key/url→nil.
- Web: `GET /api/settings` ai-block shape + key masking; `/models` with stubbed Ollama (httptest) and fallback path for Anthropic (don't call the real API in tests); `import-env` copies only requested non-empty fields and is role-gated.
- Consumers: enrich_context/prompt_suggest pick up a changed saved model without restart (resolver-backed fake).
- `npm run build` passes; existing Go tests stay green.

## Out of scope (YAGNI)

- Encrypting the stored key at rest (current behavior unchanged).
- Per-user (vs per-team) AI config.
- Provider plugins beyond Anthropic/Ollama.
- Cache eviction for the client providers.

## Risks

- The Anthropic `/v1/models` call uses the team's key server-side; failures degrade to the curated list (never block the panel).
- Pipeline gating change (always constructed, per-run key check) alters startup behavior slightly; a run with no effective key logs and skips rather than the pipeline never being constructed.
