# Configurable Embedding Provider (OpenAI / Ollama) Implementation Plan

> **For agentic workers:** Implement task-by-task with TDD. **PROJECT POLICY: DO NOT COMMIT** — every task ends at a verification checkpoint (build + tests green); the owner commits. Never run `git commit/add/push`.
>
> **Commands:** Go build `go build ./...`; pkg tests `go test ./internal/<pkg>/`; web build (from `web/`) `npm run build`. Ignore macOS noise `deprecated|sqlite3.h|cgo-gcc`.

**Goal:** Let a superadmin choose the embedding provider (OpenAI or Ollama) + model in the web UI (deployment-wide), with a custom OpenAI base URL, an admin "re-embed all" migration when the dimension changes, and fail-soft embedding so knowledge edits never 500.

**Spec:** `docs/superpowers/specs/2026-06-24-configurable-embeddings-design.md`

**Architecture:** New `OpenAIEmbedder`; a singleton `embedding_config` row (provider/model/key/base_url/ollama_url/dimension); `Sources.Embedder` reads it; stores expose `RebuildEmbeddingColumns(newDim)`; a superadmin re-embed endpoint; write paths skip embedding on failure.

---

## Task 1: Fail-soft embedding (immediate 500 fix)

**Files:** `internal/web/handlers.go` (`handleKnowledgeUpdate`, `handleKnowledgeStore`), `internal/mcp/tools.go` (`HandleKnowledgeStore`).

- [ ] **handleKnowledgeUpdate re-embed block** (~715-756): change every failure path so it does NOT 500. On `embedder == nil`, `embedder.Embed` error, or `ReplaceEntryChunks` error → `slog.Warn("knowledge update: embedding skipped", "id", id, "err", err)` and `writeJSON(w, map[string]any{"ok": true, "embedding_skipped": true})` then return. (Text is already saved.) Keep the `ErrNotFound` → 404 case for ReplaceEntryChunks.
- [ ] **handleKnowledgeStore** (~620-650): wrap the embed step so a nil embedder or an `Embed` error does not fail the store. Read how it currently embeds + calls `StoreEntryChunked`. If embedding fails, store the entry with text-only chunks (nil embeddings) and log a warning; the store must still succeed and return the new id. Confirm `StoreEntryChunked` tolerates chunks with nil `Embedding` (grep its impl in `internal/storage/sqlite.go` + `postgres.go`); if a nil-embedding chunk would error, store the entry row without chunk vectors (e.g. store text, skip the vector insert) — pick the minimal change that lets a store succeed with no embedder. Report exactly what you did.
- [ ] **MCP HandleKnowledgeStore**: same fail-soft — never fail the tool call solely because embedding failed.
- [ ] **Tests:** add/extend a web handler test proving an update whose content changed returns 200 (not 500) when the embedder errors. Use a test server whose `aiSrc` embedder returns an error or is nil (see how existing reembed/handler tests inject `aiSrc`; `newReembedServer` in `knowledge_reembed_test.go` is a good reference). Assert 200 + entry text persisted.
- [ ] **Verify:** `go build ./...`; `go test ./internal/web/ ./internal/mcp/`. Checkpoint.

---

## Task 2: OpenAI embedder + model-dimension helper

**Files:** create `internal/embedding/openai.go`, `internal/embedding/openai_test.go`; modify `internal/embedding/provider.go`.

- [ ] **Test first** (`openai_test.go`): spin a stub `httptest.Server` returning `{"data":[{"embedding":[...]}]}`; assert `NewOpenAIEmbedder(srv.URL, "k", "text-embedding-3-small").Embed(ctx, "hi")` returns the vector; assert a non-200 returns an error. Test `ModelDimension("text-embedding-3-small") == 1536`, `-large == 3072`, unknown == 0.
- [ ] **Implement `OpenAIEmbedder`** matching the `Embedder` interface (see `internal/embedding/embedder.go` for the exact method set — likely `Embed(ctx, string) ([]float32, error)` and maybe `Ping`). `NewOpenAIEmbedder(baseURL, apiKey, model string)`; POST `{baseURL}/v1/embeddings` with body `{"model":model,"input":text}` and `Authorization: Bearer <key>`; parse `data[0].embedding` (JSON numbers → []float32). baseURL default `https://api.openai.com` if empty.
- [ ] **`ModelDimension(model string) int`** in the embedding package: map known OpenAI models → dim (3-small=1536, 3-large=3072, ada-002=1536); 0 for unknown.
- [ ] **Provider accessor:** add an OpenAI-aware method to `embedding.Provider` (e.g. `OpenAIEmbedder(baseURL, apiKey, model string) Embedder`) with the same caching style as the Ollama one (key includes provider+base+model; do NOT cache the raw key in the map key — hash it or include model+base only). Leave the Ollama accessor unchanged.
- [ ] **Verify:** `go test ./internal/embedding/`; `go build ./...`. Checkpoint.

---

## Task 3: `embedding_config` storage (singleton) + seed + methods

**Files:** `internal/storage/sqlite.go` + `postgres_teams.go` (schema/seed), `internal/storage/teams.go` (type + interface), `teams_sqlite.go` + `postgres_teams.go` (Get/Put), `internal/web/server_test.go` (mock stub), tests.

- [ ] **Type** in `internal/storage/teams.go`:
  ```go
  type EmbeddingConfig struct {
      Provider     string // "openai" | "ollama"
      Model        string
      OpenAIAPIKey string
      OpenAIBaseURL string
      OllamaURL    string
      Dimension    int
      UpdatedAt    time.Time
  }
  ```
  Add to `TeamStore` interface: `GetEmbeddingConfig(ctx) (*EmbeddingConfig, error)`, `PutEmbeddingConfig(ctx, EmbeddingConfig) error`.
- [ ] **Schema (singleton, both engines)** mirroring `auth_config`: table `embedding_config(id INT PK CHECK(id=1), provider, model, openai_api_key, openai_base_url, ollama_url, dimension INT, updated_at)`. Seed one row if absent with: `provider='openai'`, `model='text-embedding-3-small'`, `openai_base_url='https://api.openai.com'`, `ollama_url` = existing env `OLLAMA_URL` if any, `dimension` = the store's `embeddingDim` (the current physical column dimension). Use the same migration mechanism as the other tables (SQLite `phase5Tables`+seed Exec; Postgres `migrateTeams` stmts list). The store has `s.embeddingDim` available at migration time.
- [ ] **Get/Put** in both engines (UPSERT on id=1 for Put; UpdatedAt=now). Mirror `GetAuthConfig`/`PutAuthConfig` style.
- [ ] **Mock stub** in `internal/web/server_test.go`: add `GetEmbeddingConfig`/`PutEmbeddingConfig` returning a zero config / nil so the package builds (extend with a field if a test needs to drive it).
- [ ] **Test** (`teams_test.go`): round-trip Put then Get; confirm the seeded default exists on a fresh store with the expected provider/model/baseURL and dimension == the store's dim.
- [ ] **Verify:** `go test ./internal/storage/`; `go build ./...`; `go test ./internal/...`. Checkpoint.

---

## Task 4: `Sources.Embedder` reads embedding_config

**Files:** `internal/aiconfig/sources.go` (+ its Resolver/Sources wiring), tests.

- [ ] Change `Sources.Embedder(ctx, teamID)` to read the deployment embedding config instead of per-team Ollama fields. `Sources` needs access to the store's `GetEmbeddingConfig` — add a small interface field to `Sources` (e.g. `EmbedConfig interface{ GetEmbeddingConfig(ctx) (*storage.EmbeddingConfig, error) }`) wired in `cmd/server/main.go` where `Sources` is constructed. Resolution:
  - `cfg.Provider == "openai"` → `s.Embed.OpenAIEmbedder(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.Model)`
  - `cfg.Provider == "ollama"` → `s.Embed.Embedder(cfg.OllamaURL, cfg.Model)`
  - error/unconfigured → `nil` (fail-soft handles nil downstream).
- [ ] Keep `ChunkConfig` unchanged.
- [ ] **Test:** with a stub embedding config returning provider=openai, `Embedder` returns a non-nil embedder; provider=ollama returns the ollama one. (Use a fake EmbedProvider that records which accessor was called.)
- [ ] **Wire** `main.go` to pass the store as the embedding-config source into `Sources`.
- [ ] **Verify:** `go test ./internal/aiconfig/`; `go build ./...`. Checkpoint.

---

## Task 5: `RebuildEmbeddingColumns(newDim)` + mutable dimension

**Files:** `internal/storage/postgres.go`, `internal/storage/sqlite.go` (+ interface in `storage.go`), tests.

- [ ] Add to the entry `Store` interface: `RebuildEmbeddingColumns(ctx, newDim int) error`.
- [ ] **Postgres:** in a tx, `DROP TABLE IF EXISTS chunk_embeddings, embeddings CASCADE` (or drop+recreate just the vector tables) and recreate them at `vector(newDim)` with their ivfflat indexes (copy the existing CREATE from `postgres.go:90-135`, parameterized by newDim). Then set `s.embeddingDim = newDim`. (Vectors are intentionally lost; re-embed repopulates.) Keep `entry_chunks` (text) intact — only the embedding tables are rebuilt; after rebuild, chunk_embeddings is empty until re-embed.
- [ ] **SQLite:** the sqlite-vec virtual table(s) for vectors — recreate analogously at the new dim (find how they're created in `sqlite.go`; drop + recreate the vec tables). Set `s.embeddingDim = newDim`.
- [ ] Make `embeddingDim` updatable (it's a struct field; just assign). Ensure concurrent safety is not required for v1 (admin action, single-shot) — note if a mutex is warranted.
- [ ] **Test (SQLite):** store an entry at dim D1; `RebuildEmbeddingColumns(D2)`; assert a subsequent `ReplaceEntryChunks` with a D2-length vector succeeds and a D1-length vector now fails the dim check.
- [ ] **Verify:** `go test ./internal/storage/`; `go build ./...`. Checkpoint.

---

## Task 6: Re-embed-all admin endpoint

**Files:** create `internal/web/embedding_handlers.go`; `internal/web/server.go` (routes); tests.

- [ ] `handleReembedAll` (superadmin): resolve configured embedder via `s.aiSrc.Embedder(ctx, "")` and the model dimension via `embedding.ModelDimension(cfg.Model)` (load cfg from `s.store.GetEmbeddingConfig`). If dim unknown (0) and provider openai → 400. If `dim != currentColumnDim` (from cfg.Dimension) → `s.store.RebuildEmbeddingColumns(ctx, dim)` and `PutEmbeddingConfig` with updated `Dimension`. Then list ALL entries (across all teams — superadmin), and for each: chunk via `s.aiSrc.ChunkConfig` + embed each chunk + `ReplaceEntryChunks`; count successes; skip+log failures. Return `{reembedded, skipped, dimension}`. If embedder is nil → 400 "embedding not configured".
- [ ] Route under `RequireSuperadmin`: `r.Post("/api/admin/reembed", s.handleReembedAll)`.
- [ ] You'll need a way to enumerate all entries: use `s.store.ListEntries(ctx, storage.ListFilter{})` (empty filter = all teams) — confirm it returns all; page if there's a limit.
- [ ] **Test:** superadmin → 200 with counts (use a store/mock with a couple entries and a stub embedder); non-superadmin → 403 (route gate).
- [ ] **Verify:** `go test ./internal/web/`; `go build ./...`. Checkpoint.

---

## Task 7: Embedding-config endpoints (GET/PUT)

**Files:** `internal/web/embedding_handlers.go` (add handlers); `internal/web/server.go` (routes); `web/src/lib/api.ts`; tests.

- [ ] `handleGetEmbeddingConfig` (superadmin): return the config with `openai_api_key` masked to `"stored"` when non-empty, `""` otherwise (mirror how `handleGetSettings` masks `anthropic_api_key`). Include a `current_dimension` (the live column dim) and the configured model's dimension (`ModelDimension`) so the UI can show a mismatch.
- [ ] `handlePutEmbeddingConfig` (superadmin): decode provider/model/openai_api_key/openai_base_url/ollama_url; if the incoming key is empty or the literal `"stored"`, preserve the existing key (don't overwrite). Persist via `PutEmbeddingConfig` (keep existing `Dimension` — only the re-embed action changes it).
- [ ] Routes under `RequireSuperadmin`: `GET/PUT /api/admin/embedding-config`.
- [ ] **api.ts:** `getEmbeddingConfig()`, `putEmbeddingConfig(cfg)`, `reembedAll()`.
- [ ] **Test:** PUT then GET round-trips (key masked); empty/"stored" key preserves the prior key.
- [ ] **Verify:** `go test ./internal/web/`; `go build ./...`; from `web/` `npm run build`. Checkpoint.

---

## Task 8: Settings UI — Embeddings section

**Files:** `web/src/pages/Settings.tsx`.

- [ ] Add a superadmin-only **Embeddings** section: provider `Select` (OpenAI / Ollama); `model` field (free text with hints for known OpenAI models); OpenAI API key field (masked — show "stored", only send when changed, same pattern as the Anthropic key in this page); OpenAI base URL field (default `https://api.openai.com`); Ollama URL field (shown when provider=ollama). Save → `putEmbeddingConfig`.
- [ ] Show the configured model's dimension vs the current column dimension (from the GET response). When they differ, show a warning and a **"Re-embed all entries"** button → `reembedAll()` with a confirm dialog and a busy/result state (shows `{reembedded, skipped}`).
- [ ] Gate the section on the viewer being superadmin (`getMe()`); hide otherwise.
- [ ] **Verify:** from `web/` `npm run build`. Manually sanity-check the section renders and Save works. Checkpoint.

---

## Final verification
- [ ] `go build ./...`, `go vet ./internal/...`, `go test ./internal/...` all green; `gofmt -l` clean on touched files.
- [ ] `cd web && npm run build` succeeds.
- [ ] Manual smoke: set provider=OpenAI + model + key in Settings; edit a knowledge item (no 500 even before re-embed — fail-soft); click Re-embed all; confirm search works.
- [ ] Leave everything uncommitted.
