# Configurable embedding provider (OpenAI / Ollama) — design

**Date:** 2026-06-24
**Status:** Approved (pending spec review)

## Problem

Embeddings are hard-wired to Ollama (`aiconfig.Sources.Embedder` → `embedding.Provider.Embedder(OllamaURL, OllamaModel)`; the only embedder is `internal/embedding/ollama.go`). On a deployment without Ollama, every embed call fails — editing/storing knowledge returns 500 (the re-embed path in `handleKnowledgeUpdate`). Anthropic offers no embeddings API, so "use Anthropic" is impossible for embeddings; the user wants to choose **OpenAI or Ollama**, configured in the web UI, with model choice.

LLM/generation is out of scope: it is already switchable to Anthropic per-team via Settings → LLM Provider (+ model).

## Decisions (locked)

- Support **OpenAI** and **Ollama** embedders, selectable in the web UI with model choice (+ OpenAI API key).
- Embedding config is **deployment-wide** (superadmin), not per-team — all teams share one set of fixed-dimension pgvector columns, so mixed dimensions are impossible.
- Changing the model's **dimension** triggers a re-embed via an explicit **admin "Re-embed all" button** that recreates the vector columns at the new dimension and re-embeds every entry.
- Embedding is **fail-soft**: if the embedder is unconfigured/unreachable/errors, store and edit still succeed (text saved, vectors skipped, warning logged) — no 500.
- **Default provider is OpenAI** (model `text-embedding-3-small`, 1536) when seeding a fresh config — except seed `dimension` from the existing columns / `EMBEDDING_DIM` so we never misreport the physical column dimension; the UI then surfaces the model-vs-column dimension mismatch and prompts re-embed.
- A **custom OpenAI-compatible base URL** is supported (Azure OpenAI / proxy), defaulting to `https://api.openai.com`.

## Components

### 1. OpenAI embedder
`internal/embedding/openai.go` — `OpenAIEmbedder` implementing the existing `Embedder` interface (`Embed(ctx, text) ([]float32, error)`, plus a `Ping`/health like Ollama). Calls `POST {baseURL}/v1/embeddings` with `{model, input}` and `Authorization: Bearer <key>`, where `baseURL` defaults to `https://api.openai.com` and is overridable (Azure/proxy). Constructor: `NewOpenAIEmbedder(baseURL, apiKey, model string)`. Known dimensions: `text-embedding-3-small`=1536, `text-embedding-3-large`=3072, `text-embedding-ada-002`=1536. A `ModelDimension(model string) int` helper (in the embedding package) maps known models → dimension (0/unknown ⇒ caller must treat as unknown).

`internal/embedding/provider.go` Provider gains an OpenAI-aware cache (key by provider|base|model|keyhash) or a parallel `OpenAIEmbedder(key, model)` accessor. Keep Ollama path unchanged.

### 2. Deployment-wide embedding config (singleton row)
New singleton table `embedding_config` (mirrors the `auth_config` singleton pattern):
```
id INT PK CHECK(id=1), provider TEXT ('ollama'|'openai'), model TEXT,
openai_api_key TEXT, openai_base_url TEXT, ollama_url TEXT, dimension INT, updated_at
```
`openai_base_url` defaults to `https://api.openai.com` (override for Azure OpenAI / proxies). The default seed sets `provider='openai'`, `model='text-embedding-3-small'`, and `dimension` = the existing `EMBEDDING_DIM`/current column dimension.
- `dimension` = the dimension the vector columns are CURRENTLY built at (source of truth for validation; set by the re-embed migration).
- Storage methods on a new interface segment: `GetEmbeddingConfig(ctx)`, `PutEmbeddingConfig(ctx, cfg)`. Seed a default row from env (`EMBEDDING_DIM`, existing `OLLAMA_URL`/model) on first run so behavior is unchanged for existing Ollama deployments.
- Endpoints (superadmin): `GET /api/admin/embedding-config`, `PUT /api/admin/embedding-config`. The API key is masked (`"stored"`) in GET, like `anthropic_api_key` in team settings.

### 3. Embedder resolution
`Sources.Embedder(ctx, teamID)` ignores per-team settings for embeddings and instead reads `embedding_config`:
- `provider == "openai"` → `OpenAIEmbedder(apiKey, model)`
- `provider == "ollama"` → `OllamaEmbedder(url, model)` (current behavior)
- returns `nil` if unconfigured.
`ChunkConfig` stays as-is (per-team chunking is fine; dimension is global).

### 4. Active dimension is runtime-mutable
`PostgresStore`/`SQLiteStore` `embeddingDim` is initialized from `embedding_config.dimension` (fallback `EMBEDDING_DIM` env) at startup; vector columns are created at that dimension if absent. A method `RebuildEmbeddingColumns(ctx, newDim int) error` drops and recreates `embeddings` + `chunk_embeddings` (and any vec index) at `newDim` and updates the in-memory `embeddingDim`. (SQLite uses the sqlite-vec virtual table; recreate analogously.)

### 5. Re-embed admin action
`POST /api/admin/reembed` (superadmin):
1. Resolve configured embedder + its model dimension `D`.
2. If `D == 0` (unknown model) → 400 "unknown embedding model dimension".
3. If `D != store.embeddingDim` → `RebuildEmbeddingColumns(ctx, D)` and update `embedding_config.dimension = D`.
4. Iterate all entries: chunk (ChunkConfig) → embed each chunk → `ReplaceEntryChunks`. Skip+log entries whose embed fails; collect a count.
5. Return `{reembedded, skipped, dimension}`.
Runs synchronously; for large datasets this is long — acceptable for now, logged with progress; documented as a follow-up to background it.

Settings UI shows the configured model's dimension vs the current column dimension; when they differ, a warning + **"Re-embed all entries"** button calls this endpoint. The button is also offered after switching provider/model.

### 6. Fail-soft embedding
Make embed failures non-fatal in the write paths:
- `handleKnowledgeUpdate` re-embed block: on `embedder == nil` OR `Embed` error OR `ReplaceEntryChunks` error → log `slog.Warn` and return `{ok:true, embedding_skipped:true}` (200) instead of 500. (Text already saved.)
- `handleKnowledgeStore` (web) and MCP `HandleKnowledgeStore`: if embedder is nil or Embed errors, store the entry with no/empty embedding (or chunk content without vectors) and log a warning — never fail the store on embedding alone. Confirm `StoreEntryChunked` tolerates chunks with nil embeddings (or store text-only); adjust if needed so a nil-embedding chunk is allowed and simply not added to the vector table.

### 7. Settings UI
`web/src/pages/Settings.tsx` gains an **Embeddings** section (superadmin-only): provider dropdown (Ollama / OpenAI), model field (free text + known-model hints), OpenAI API key (masked), Ollama URL (when Ollama). Save → `PUT /api/admin/embedding-config`. Shows current vs configured dimension and the re-embed button when they differ. API helpers in `api.ts`: `getEmbeddingConfig`, `putEmbeddingConfig`, `reembedAll`.

## Out of scope
- Per-team embedding providers/dimensions (impossible with shared fixed-dim columns).
- Backgrounding the re-embed job (synchronous for v1).
- Anthropic embeddings (no API exists).
- LLM provider/model (already configurable).

## Testing
- `OpenAIEmbedder` against a stub HTTP server (success + error + dimension); `ModelDimension` mapping.
- `embedding_config` storage round-trip + default seed.
- `Sources.Embedder` selects the right embedder per provider.
- `RebuildEmbeddingColumns` changes the effective dimension; `ReplaceEntryChunks` then accepts the new dim and rejects the old.
- Endpoints: GET/PUT embedding-config (superadmin gate; key masking); `/api/admin/reembed` (superadmin; returns counts).
- Fail-soft: update/store with a nil/erroring embedder returns 200 and persists text (no 500).
- Web build.
