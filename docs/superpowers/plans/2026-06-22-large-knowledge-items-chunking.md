# Large Knowledge Items via Transparent Multi-Vector Chunking — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Accept arbitrarily large knowledge items by chunking content internally into multiple embedding vectors, while keeping each item a single logical entry (one ID/rating/usage).

**Architecture:** Add a per-team embedding/chunking config. Add a pure chunking engine. Extend the storage layer with `entry_chunks` + a per-chunk vector table and a new `StoreEntryChunked` method (the existing single-vector `StoreEntry` is retained and delegates to it, so callers don't break). Vector search runs over chunks and dedupes to one entry. The MCP `knowledge_store` description is rewritten per-request to telegraph the team's effective size behavior.

**Tech Stack:** Go, `mark3labs/mcp-go` v0.54.1, SQLite (`sqlite-vec`) + Postgres (`pgvector`), `tiktoken-go/tokenizer` (offline token counting), React/TS web.

**Spec:** `docs/superpowers/specs/2026-06-22-large-knowledge-items-chunking-design.md`

**Build/test gate (run from repo root, this is the Go server — NOT the ./api SAM project):**
- `go build ./...`
- `go test ./...`
- Web: `cd web && npm run build`

---

## File Structure

- `internal/embedding/chunk.go` (new) — pure chunking engine + token counting.
- `internal/embedding/chunk_test.go` (new) — chunk engine tests.
- `internal/config/config.go` (modify) — three new env-backed config fields.
- `internal/aiconfig/aiconfig.go` (modify) — resolve the three fields into `EffectiveConfig`.
- `internal/aiconfig/sources.go` (modify) — expose effective chunk config for a team.
- `internal/storage/teams.go` (modify) — three new `TeamSettings` fields.
- `internal/storage/sqlite.go` (modify) — schema, `StoreEntryChunked`, chunk-based search, delete, replace-chunks, backfill.
- `internal/storage/postgres.go` / `postgres_search.go` (modify) — Postgres mirror.
- `internal/storage/storage.go` (modify) — `EntryChunk` type + `StoreEntryChunked` interface method.
- `internal/mcp/tools.go` (modify) — chunk+embed write path; response chunk count.
- `internal/mcp/server.go` (modify) — `WithToolFilter` for dynamic `knowledge_store` description.
- `internal/web/handlers.go` (modify) — re-embed on content update.
- `internal/web/settings.go` + `internal/web/ai_settings.go` (modify) — accept/return new fields.
- `web/src/pages/Settings.tsx` (modify) — UI inputs.

---

## Task 1: Storage type + interface for chunked entries

**Files:**
- Modify: `internal/storage/storage.go`

- [ ] **Step 1: Add the `EntryChunk` type and interface method.** In `storage.go`, after the `SearchResult` struct, add:

```go
// EntryChunk is one embedded slice of a knowledge entry's content.
// Index 0 is the representative chunk (used for pipeline clustering and the
// legacy per-entry vector). An entry that fits in one chunk has exactly one.
type EntryChunk struct {
	Index         int
	Content       string
	TokenEstimate int
	Embedding     []float32 // len must equal the store's embeddingDim, or nil
}
```

In the `Store` interface, directly below the `StoreEntry` method, add:

```go
	// StoreEntryChunked creates a new entry whose content is represented by one
	// or more embedding vectors (chunks). chunks[0] is the representative chunk.
	// Assigns a fresh UUID; the entry.ID field is ignored. Returns the new ID.
	StoreEntryChunked(ctx context.Context, entry KnowledgeEntry, chunks []EntryChunk) (string, error)
	// ReplaceEntryChunks atomically replaces all chunks (and vectors) for an
	// existing entry. Used when an entry's content is edited. Returns ErrNotFound.
	ReplaceEntryChunks(ctx context.Context, entryID string, chunks []EntryChunk) error
```

- [ ] **Step 2: Build to confirm the interface change is visible.**

Run: `go build ./internal/storage/`
Expected: FAIL — `*SQLiteStore` and `*PostgresStore` do not implement `Store` (missing `StoreEntryChunked`, `ReplaceEntryChunks`). This confirms both impls must be updated (Tasks 3 & 3b).

(No commit yet — leave the build red until Task 3 makes it green.)

---

## Task 2: Chunking engine (TDD)

**Files:**
- Create: `internal/embedding/chunk.go`
- Create: `internal/embedding/chunk_test.go`
- Modify: `go.mod` / `go.sum` (add tokenizer dependency)

**Dependency note:** Use a self-contained, offline token counter — the server must not fetch BPE files over the network at runtime. Prefer `github.com/tiktoken-go/tokenizer` (embeds cl100k_base). If init fails for any reason, fall back to a `len(runes)/4` heuristic. Exactness is not required (its vocab does not match `nomic-embed-text`); we only need a stable proxy plus the 0.9 safety margin.

- [ ] **Step 1: Write failing tests.** Create `internal/embedding/chunk_test.go`:

```go
package embedding

import (
	"strings"
	"testing"
)

func TestChunk_SmallContentSingleChunk(t *testing.T) {
	cfg := ChunkConfig{MaxTokens: 1000, OverlapTokens: 50, MaxChunks: 10}
	got := Chunk("hello world", cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(got))
	}
	if got[0].Content != "hello world" {
		t.Fatalf("content altered: %q", got[0].Content)
	}
	if got[0].Index != 0 {
		t.Fatalf("want index 0, got %d", got[0].Index)
	}
}

func TestChunk_LargeContentSplits(t *testing.T) {
	// ~20k words => well over an 8-token-per-chunk budget => many chunks.
	body := strings.Repeat("paragraph one.\n\nparagraph two.\n\n", 500)
	cfg := ChunkConfig{MaxTokens: 64, OverlapTokens: 8, MaxChunks: 1000}
	got := Chunk(body, cfg)
	if len(got) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(got))
	}
	for i, c := range got {
		if c.Index != i {
			t.Fatalf("chunk %d has index %d", i, c.Index)
		}
		if c.Content == "" {
			t.Fatalf("chunk %d empty", i)
		}
	}
}

func TestChunk_NoContentLoss(t *testing.T) {
	// Concatenating chunk bodies (minus overlap) must preserve all original runes.
	body := strings.Repeat("The quick brown fox. ", 2000)
	cfg := ChunkConfig{MaxTokens: 32, OverlapTokens: 0, MaxChunks: 100000}
	got := Chunk(body, cfg)
	var sb strings.Builder
	for _, c := range got {
		sb.WriteString(c.Content)
	}
	// With zero overlap, no rune may be dropped.
	if normalizeWS(sb.String()) != normalizeWS(body) {
		t.Fatalf("content lost: joined len=%d orig len=%d", sb.Len(), len(body))
	}
}

func TestChunk_MaxChunksAbsorbsRemainder(t *testing.T) {
	body := strings.Repeat("word ", 10000)
	cfg := ChunkConfig{MaxTokens: 16, OverlapTokens: 0, MaxChunks: 3}
	got := Chunk(body, cfg)
	if len(got) != 3 {
		t.Fatalf("want exactly 3 chunks (cap), got %d", len(got))
	}
	// The final chunk must carry the leftover so nothing is dropped.
	var sb strings.Builder
	for _, c := range got {
		sb.WriteString(c.Content)
	}
	if normalizeWS(sb.String()) != normalizeWS(body) {
		t.Fatalf("content dropped under MaxChunks cap")
	}
}

func TestChunk_Unicode(t *testing.T) {
	body := strings.Repeat("日本語のテキストです。", 1000)
	cfg := ChunkConfig{MaxTokens: 16, OverlapTokens: 0, MaxChunks: 100000}
	got := Chunk(body, cfg)
	for _, c := range got {
		if !validUTF8(c.Content) {
			t.Fatalf("chunk split a multibyte rune")
		}
	}
}

func normalizeWS(s string) string { return strings.Join(strings.Fields(s), " ") }
```

- [ ] **Step 2: Run tests to confirm they fail (compile error — `Chunk`/`ChunkConfig`/helpers undefined).**

Run: `go test ./internal/embedding/ -run TestChunk -v`
Expected: FAIL (undefined: Chunk, ChunkConfig).

- [ ] **Step 3: Implement `internal/embedding/chunk.go`.** Requirements the implementation must satisfy (write real code, no placeholders):

```go
package embedding

import (
	"unicode/utf8"
	// tokenizer import — see dependency note
)

type ChunkConfig struct {
	MaxTokens     int // per-chunk token budget (embedding model context window)
	OverlapTokens int // tokens of trailing context repeated into the next chunk
	MaxChunks     int // 0 = unlimited
}

type Chunk struct {
	Index         int
	Content       string
	TokenEstimate int
}

// safetyFraction keeps us below the model's true limit since the tokenizer
// vocab does not match the embedding model's.
const safetyFraction = 0.9

// CountTokens returns an estimated token count for s. Uses the offline
// tokenizer; falls back to len([]rune(s))/4 if the tokenizer is unavailable.
func CountTokens(s string) int { /* tokenizer.Encode, else rune/4 */ }

// Chunk splits content into coherent chunks no larger than ~MaxTokens*safetyFraction.
// Splitting prefers, in order: markdown headings (lines starting with '#'),
// blank-line paragraph boundaries, sentence boundaries, then a hard rune-window
// fallback. Each chunk after the first is prefixed with up to OverlapTokens of
// the previous chunk's trailing text. If the number of chunks would exceed
// MaxChunks (>0), the final chunk absorbs all remaining content. Content that
// fits in one chunk is returned unchanged as a single chunk (Index 0).
// Never splits inside a UTF-8 rune. Never drops content.
func Chunk(content string, cfg ChunkConfig) []Chunk { /* ... */ }

// test helpers
func validUTF8(s string) bool { return utf8.ValidString(s) }
```

Implementation guidance:
- Effective budget = `int(float64(cfg.MaxTokens) * safetyFraction)`; clamp to >=1.
- Fast path: if `CountTokens(content) <= budget` return `[]Chunk{{Index:0, Content:content, TokenEstimate:CountTokens(content)}}`.
- Otherwise build a list of "atoms" by splitting on the boundary hierarchy, then greedily pack atoms into chunks until adding the next would exceed `budget`. If a single atom alone exceeds `budget`, hard-split it on rune windows sized to the budget.
- Track a running chunk count; when `cfg.MaxChunks>0` and you are on the last allowed chunk, append all remaining atoms into it regardless of budget.
- Overlap: when starting chunk N>0, prepend the last `OverlapTokens` worth of runes from chunk N-1's content. (Overlap means the joined-bodies test uses `OverlapTokens:0`; do not add overlap when `OverlapTokens==0`.)

- [ ] **Step 4: Run tests to confirm they pass.**

Run: `go test ./internal/embedding/ -run TestChunk -v`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Tidy modules and build.**

Run: `go mod tidy && go build ./internal/embedding/`
Expected: success.

- [ ] **Step 6: Commit.**

```bash
git add internal/embedding/chunk.go internal/embedding/chunk_test.go go.mod go.sum
git commit -m "feat(embedding): add content chunking engine with offline token counting"
```

---

## Task 3: SQLite multi-vector storage (TDD)

**Files:**
- Modify: `internal/storage/sqlite.go`
- Test: `internal/storage/sqlite_chunks_test.go` (new)

- [ ] **Step 1: Add schema for chunk tables** in `migrate()` (after the `entry_embeddings` block, before the idempotent ALTERs):

```go
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS entry_chunks (
			rowid          INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id       TEXT    NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			chunk_index    INTEGER NOT NULL,
			content        TEXT    NOT NULL,
			token_estimate INTEGER NOT NULL DEFAULT 0,
			UNIQUE(entry_id, chunk_index)
		);
	`)
	if err != nil {
		return fmt.Errorf("create entry_chunks table: %w", err)
	}
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
			embedding FLOAT[%d]
		);
	`, s.embeddingDim))
	if err != nil {
		return fmt.Errorf("create vec_chunks table: %w", err)
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_entry_chunks_entry ON entry_chunks(entry_id)`)
	if err != nil {
		return fmt.Errorf("index entry_chunks: %w", err)
	}
```

Then add a call `if err := s.backfillChunks(); err != nil { return fmt.Errorf("backfill chunks: %w", err) }` at the end of `migrate()` (before `return nil`).

- [ ] **Step 2: Write failing tests** in `internal/storage/sqlite_chunks_test.go`. Use the existing test setup pattern from the other `*_test.go` files in this package (open a temp-file SQLite store with the same `embeddingDim`). Tests:
  - `TestStoreEntryChunked_MultipleChunksOneEntry`: store an entry with 3 chunks (distinct embeddings); `GetEntry` returns the single entry; a direct `SELECT COUNT(*) FROM entry_chunks WHERE entry_id=?` returns 3.
  - `TestSearchSimilar_DedupesToEntry`: store one entry with 3 chunks where chunk 1's embedding equals the query vector; `SearchSimilar(query, 5)` returns that entry exactly once.
  - `TestStoreEntry_BackwardCompatSingleChunk`: the legacy `StoreEntry(ctx, entry, emb)` path produces exactly one `entry_chunks` row and is searchable.
  - `TestReplaceEntryChunks`: store 1 chunk, replace with 2 chunks, assert chunk count is now 2 and search finds the new vector but not the old one.
  - `TestDeleteEntry_RemovesChunks`: after `DeleteEntry`, `entry_chunks` and `vec_chunks` rows for that entry are gone.

Run: `go test ./internal/storage/ -run 'Chunk|DedupesToEntry|BackwardCompat|ReplaceEntry' -v`
Expected: FAIL (compile — methods undefined).

- [ ] **Step 3: Implement the storage methods** in `sqlite.go`:

`StoreEntryChunked` — like `StoreEntry` but: insert the entry row (same INSERT as current `StoreEntry`), then for each chunk insert into `entry_chunks (entry_id, chunk_index, content, token_estimate)` and, when `chunk.Embedding != nil`, into `vec_chunks (rowid, embedding)` using the `entry_chunks` rowid. For `chunks[0]` also write the representative vector to `vec_entries` and `entry_embeddings` (keyed by the *entries* rowid) exactly as the current `StoreEntry` does, so the pipeline's `GetAllEmbeddings` and any legacy reader keep working. Validate each non-nil embedding length == `s.embeddingDim`. All in one transaction.

Refactor `StoreEntry` to delegate:
```go
func (s *SQLiteStore) StoreEntry(ctx context.Context, entry KnowledgeEntry, embedding []float32) (string, error) {
	return s.StoreEntryChunked(ctx, entry, []EntryChunk{{Index: 0, Content: entry.Content, Embedding: embedding}})
}
```

`SearchSimilar` — change the vector query from `vec_entries` to `vec_chunks`, then map each matched chunk rowid → `entry_id` via `entry_chunks`, keep the **minimum distance per entry** (dedupe), then fetch entries by id (preserving best-distance order). Keep the `embeddingDim` guard. Return one `SearchResult` per entry.

`ReplaceEntryChunks` — in a tx: look up the entry rowid by id (return `ErrNotFound` if missing); delete `vec_chunks` rows whose rowid is in `(SELECT rowid FROM entry_chunks WHERE entry_id=?)`; delete `entry_chunks WHERE entry_id=?`; re-insert the provided chunks (same as in `StoreEntryChunked`); refresh the representative vector in `vec_entries`/`entry_embeddings` from `chunks[0]`.

`DeleteEntry` — extend the existing tx to also delete `vec_chunks` rows for the entry's chunks and `entry_chunks WHERE entry_id=?` (before deleting the entry row).

`backfillChunks` — idempotent: for every entry that has no row in `entry_chunks`, insert a chunk 0 with `content = entries.content`; if the entry has a vector in `entry_embeddings`, copy it into `vec_chunks` keyed by the new `entry_chunks` rowid. Guard with `WHERE NOT EXISTS (SELECT 1 FROM entry_chunks WHERE entry_chunks.entry_id = entries.id)` so re-runs are no-ops.

- [ ] **Step 4: Run tests.**

Run: `go test ./internal/storage/ -run 'Chunk|DedupesToEntry|BackwardCompat|ReplaceEntry' -v`
Expected: PASS.

- [ ] **Step 5: Run the full storage suite to catch regressions** (search/pipeline/import tests).

Run: `go test ./internal/storage/ -v`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/storage/sqlite.go internal/storage/storage.go internal/storage/sqlite_chunks_test.go
git commit -m "feat(storage): SQLite multi-vector chunk storage with deduped search"
```

---

## Task 3b: Postgres multi-vector storage

**Files:**
- Modify: `internal/storage/postgres.go`, `internal/storage/postgres_search.go`

- [ ] **Step 1: Mirror the SQLite work in Postgres.** Add an `entry_chunks` table (`id BIGSERIAL PRIMARY KEY, entry_id TEXT REFERENCES entries(id) ON DELETE CASCADE, chunk_index INT, content TEXT, token_estimate INT, UNIQUE(entry_id, chunk_index)`) and a chunk embeddings table using `vector(dim)` mirroring the existing per-entry embeddings table. Implement `StoreEntryChunked`, `ReplaceEntryChunks`, chunk-based deduped `SearchSimilar`, extend `DeleteEntry`, and an idempotent chunk backfill. Keep the representative per-entry embedding table populated from `chunks[0]` so existing pipeline queries are unchanged. Refactor `StoreEntry` to delegate to `StoreEntryChunked` with a single chunk.

- [ ] **Step 2: Build.**

Run: `go build ./internal/storage/`
Expected: success — both stores now satisfy the `Store` interface (Task 1's red build goes green).

- [ ] **Step 3: Run available Postgres tests** (skip if the suite requires a live PG and one is not configured — note this in the task report).

Run: `go test ./internal/storage/ -run Postgres -v`
Expected: PASS or SKIP (document which).

- [ ] **Step 4: Commit.**

```bash
git add internal/storage/postgres.go internal/storage/postgres_search.go
git commit -m "feat(storage): Postgres multi-vector chunk storage mirror"
```

---

## Task 4: Per-team chunk config plumbing (TDD)

**Files:**
- Modify: `internal/config/config.go`, `internal/storage/teams.go`,
  `internal/storage/sqlite.go` (team_settings ALTERs), `internal/storage/postgres_teams.go`,
  `internal/aiconfig/aiconfig.go`, `internal/aiconfig/sources.go`,
  `internal/web/settings.go`, `internal/web/ai_settings.go`

- [ ] **Step 1: Config defaults.** In `config.go` add fields `EmbeddingMaxTokens int`, `ChunkOverlapTokens int`, `MaxChunks int` with env parsing in `Load()`: `EMBEDDING_MAX_TOKENS` (default 8192), `EMBEDDING_CHUNK_OVERLAP` (default 128), `EMBEDDING_MAX_CHUNKS` (default 64). Follow the existing int-parse pattern used for `EMBEDDING_DIM`.

- [ ] **Step 2: TeamSettings fields.** In `teams.go` `TeamSettings` add `EmbeddingMaxTokens int`, `ChunkOverlapTokens int`, `MaxChunks int`.

- [ ] **Step 3: Schema.** In `sqlite.go` `teamSettingsAlters`, append:
```go
		"ALTER TABLE team_settings ADD COLUMN embedding_max_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE team_settings ADD COLUMN chunk_overlap_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE team_settings ADD COLUMN max_chunks           INTEGER NOT NULL DEFAULT 0",
```
Update `GetTeamSettings`/`PutTeamSettings` SELECT/INSERT column lists in both `sqlite.go` and `postgres_teams.go` to include the three columns (0 means "unset → fall back to env default"). Mirror the column adds in Postgres.

- [ ] **Step 4: Resolver.** In `aiconfig.go` add the three to `EffectiveConfig` and `Effective()`, resolving `saved (if >0) else env default`. Add a helper on `Sources` (sources.go), e.g. `ChunkConfig(ctx, teamID) embedding.ChunkConfig`, returning the effective values.

- [ ] **Step 5: Test the resolver.** Add a test asserting: with no saved team value, `Effective` yields the env defaults; with a saved value >0, the saved value wins. Run: `go test ./internal/aiconfig/ -v` → PASS.

- [ ] **Step 6: Web settings.** In `settings.go` `handlePutSettings` accept the three ints; in `ai_settings.go` return them in the enriched settings payload. Validate non-negative.

- [ ] **Step 7: Build + test.**

Run: `go build ./... && go test ./internal/config/ ./internal/aiconfig/ ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 8: Commit.**

```bash
git add internal/config internal/storage/teams.go internal/storage/sqlite.go internal/storage/postgres_teams.go internal/aiconfig internal/web/settings.go internal/web/ai_settings.go
git commit -m "feat(config): per-team embedding/chunking settings"
```

---

## Task 5: Wire chunking into the write path (TDD)

**Files:**
- Modify: `internal/mcp/tools.go`

- [ ] **Step 1: Update `HandleKnowledgeStore`.** After resolving the embedder and before storing: compute `cfg := src.ChunkConfig(ctx, teamID)`; `chunks := embedding.Chunk(content, cfg)`; for each chunk call `embedder.Embed(ctx, chunk.Content)` and build `[]storage.EntryChunk{Index, Content, TokenEstimate, Embedding}`; call `store.StoreEntryChunked(ctx, entry, entryChunks)`. If any embed fails, return the error (no partial store — the storage tx rolls back). Keep the existing "embedding not configured" guard.

- [ ] **Step 2: Report chunk count.** Include `chunk_count` and effective `embedding_max_tokens` in the success result text/JSON so clients get feedback (e.g. append `" (stored as N chunk(s))"`).

- [ ] **Step 3: Test.** Add/extend an mcp package test (table-driven, using the existing in-memory/sqlite test store and a stub embedder) asserting that storing content larger than a tiny configured `MaxTokens` yields >1 chunk row and a `chunk_count>1` in the response, while small content yields 1.

Run: `go test ./internal/mcp/ -run KnowledgeStore -v`
Expected: PASS.

- [ ] **Step 4: Commit.**

```bash
git add internal/mcp/tools.go internal/mcp/*_test.go
git commit -m "feat(mcp): chunk and multi-embed content on knowledge_store"
```

---

## Task 6: Re-embed on content update (bug fix, TDD)

**Files:**
- Modify: `internal/web/handlers.go` (`handleKnowledgeUpdate`, ~line 629)

- [ ] **Step 1: Re-chunk + re-embed on content change.** When the update changes `Content`, after `UpdateEntry` succeeds, resolve the team's `ChunkConfig`, chunk the new content, embed each chunk, and call `store.ReplaceEntryChunks(ctx, id, chunks)`. If no embedder is configured, skip re-embedding (leave a log line) rather than failing the edit.

- [ ] **Step 2: Test.** Add a handler/storage test: store content A, update to a longer content B, assert search now matches a vector derived from B and the stale A vector no longer matches.

Run: `go test ./internal/web/ -run Update -v`
Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add internal/web/handlers.go internal/web/*_test.go
git commit -m "fix(web): re-embed knowledge content on update"
```

---

## Task 7: Telegraph the limit in the tool description (TDD)

**Files:**
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Register a context-aware tool filter.** When constructing the `*server.MCPServer`, add `server.WithToolFilter(func(ctx context.Context, tools []mcp.Tool) []mcp.Tool { ... })`. In the filter, resolve the team via `auth.GetTeamContext(ctx)` (empty over stdio → use the default-team/env effective value), get effective `embedding_max_tokens`, and for the tool named `knowledge_store` return a copy with its `Description` rewritten from a template that names the number, e.g.:

> "...Content of any length is accepted — items larger than ~N tokens are automatically split into linked chunks internally and remain fully searchable as a single entry; do not pre-trim or split content yourself."

Leave other tools unchanged. Keep the static registered description as a sensible default.

- [ ] **Step 2: Test.** Unit-test the filter function directly: given a context carrying a team whose effective `embedding_max_tokens` is a known value, the returned `knowledge_store` tool description contains that number; other tools are returned unmodified.

Run: `go test ./internal/mcp/ -run ToolFilter -v`
Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add internal/mcp/server.go internal/mcp/*_test.go
git commit -m "feat(mcp): telegraph effective content limit in knowledge_store description"
```

---

## Task 8: Web Settings UI

**Files:**
- Modify: `web/src/pages/Settings.tsx`

- [ ] **Step 1: Add inputs** for Embedding Max Tokens, Chunk Overlap Tokens, and Max Chunks near the existing Ollama fields, wired to the settings GET/PUT payload added in Task 4. Use the existing field components/patterns in that file. Empty/0 means "use server default".

- [ ] **Step 2: Build the web app.**

Run: `cd web && npm run build`
Expected: build succeeds with no type errors.

- [ ] **Step 3: Commit.**

```bash
git add web/src/pages/Settings.tsx
git commit -m "feat(web): embedding/chunking settings inputs"
```

---

## Task 9: Full verification + docs

- [ ] **Step 1: Full build and test.**

Run: `go build ./... && go test ./...`
Expected: PASS (note any pre-existing failures unrelated to this work).

- [ ] **Step 2: Web build.**

Run: `cd web && npm run build`
Expected: PASS.

- [ ] **Step 3: Update CHANGELOG.md** with an entry summarizing: configurable per-team embedding/chunking, automatic internal chunking of large knowledge items, re-embed on update, and dynamic tool-description size hints.

- [ ] **Step 4: Update `.env.example`** with `EMBEDDING_MAX_TOKENS`, `EMBEDDING_CHUNK_OVERLAP`, `EMBEDDING_MAX_CHUNKS` and brief comments.

- [ ] **Step 5: Commit.**

```bash
git add CHANGELOG.md .env.example
git commit -m "docs: document embedding/chunking configuration"
```

---

## Self-Review Notes

- **Spec coverage:** larger items (Tasks 2,3,5), user-configurable size (Task 4), telegraphed limits (Task 7), MCP-side splitting with linkage via single-entry chunks (Tasks 3,5), update re-embed bug (Task 6). All covered.
- **Backward compatibility:** `StoreEntry` retained as a single-chunk delegator; representative vector still written to `vec_entries`/`entry_embeddings` so the analysis pipeline is untouched; backfill makes existing rows searchable via chunks.
- **Type consistency:** `EntryChunk` (storage) vs `Chunk` (embedding) are intentionally distinct; the mcp write path maps `embedding.Chunk` → `storage.EntryChunk`.
