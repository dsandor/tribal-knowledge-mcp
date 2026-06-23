# Large Knowledge Items via Transparent Multi-Vector Chunking

**Date:** 2026-06-22
**Status:** Design — awaiting review
**Author:** brainstormed with David Sandor

## Problem

MCP clients (LLMs) hit a size ceiling when storing knowledge items. The symptom
is that clients drop context from an item or split one logical learning across
several `knowledge_store` calls. Root cause: every knowledge item's entire
`content` field is embedded as a **single vector** with **no chunking and no
length guard** (`internal/embedding/ollama.go`). When content exceeds the
embedding model's context window (`nomic-embed-text` ≈ 8192 tokens), **Ollama
silently truncates** the input — the tail of the content never enters the
vector and becomes unsearchable. Clients have learned to compensate by trimming
or fragmenting, which is exactly the behavior we want to eliminate.

## Goals

1. Accept knowledge items of effectively any size without losing searchability.
2. Let a team configure the embedding/content sizing behavior (per-team,
   consistent with how Ollama URL/model are configured today).
3. Telegraph the effective size behavior in the MCP tool descriptions so clients
   stop pre-emptively trimming/splitting.
4. Have the MCP server perform any necessary splitting **internally** and keep
   the result a single logical knowledge item (one ID, one rating, one usage
   count). No client-visible fragmentation.

## Non-Goals

- A new per-user settings layer. Configuration stays per-team (decided during
  brainstorming). See [Effective AI Config design](2026-06-10-effective-ai-config-design.md).
- Splitting oversized content into multiple separate, client-visible knowledge
  items. We keep one item; chunking is an internal storage detail.
- Switching embedding providers or adding non-Ollama embedding support.

## Key Decisions

| Decision | Choice |
|---|---|
| Storage model | One knowledge item, internally multiple embedding vectors (chunks). Standard RAG multi-vector approach. |
| Config scope | Per-team (`team_settings` → env-default resolver), same pattern as existing AI settings. |
| Token counting | `tiktoken-go` for token counts, with a safety margin applied because its vocab does not match `nomic-embed-text`. |
| When to chunk | Only when content exceeds the effective limit. Items under the limit are stored exactly as today (single chunk, single vector). Fully backward compatible. |
| Telegraphing | Context-aware `WithToolFilter` rewrites the `knowledge_store` description per request from the team's effective config (HTTP = per-team; stdio = default-team/env). |

## Architecture

### Component 1 — Embedding config fields (per-team)

Add three fields to the effective-config chain. They flow through the existing
resolver (`internal/aiconfig`): **saved team setting → env default → built-in
default**.

| Field | Env var | Default | Meaning |
|---|---|---|---|
| `embedding_max_tokens` | `EMBEDDING_MAX_TOKENS` | `8192` | Per-chunk token budget = the embedding model's context window. |
| `chunk_overlap_tokens` | `EMBEDDING_CHUNK_OVERLAP` | `128` | Tokens of trailing context repeated at the start of each subsequent chunk, for continuity. |
| `max_chunks` | `EMBEDDING_MAX_CHUNKS` | `64` | Safety cap on chunks per item. `0` = unlimited. |

Touch points:
- `internal/config/config.go` — add env parsing + defaults.
- `internal/storage/teams.go` (`TeamSettings`) — add the three fields.
- `internal/storage/sqlite.go` + `internal/storage/postgres_teams.go` — idempotent
  `ALTER TABLE team_settings ADD COLUMN ...` (mirror the existing AI-column pattern).
- `internal/aiconfig/aiconfig.go` (`EffectiveConfig`, `Effective`) — resolve the
  three fields.
- `internal/web/settings.go` + `internal/web/ai_settings.go` — accept/return the
  fields; surface in the Settings UI (`web/src/pages/Settings.tsx`) near the
  Ollama fields.

### Component 2 — Chunking engine (new `internal/embedding/chunk.go`)

Pure function, independently unit-testable:

```go
type ChunkConfig struct {
    MaxTokens     int
    OverlapTokens int
    MaxChunks     int // 0 = unlimited
}

type Chunk struct {
    Index         int
    Content       string
    TokenEstimate int
}

// Chunk splits content into one or more coherent chunks. If the content fits
// within MaxTokens (with safety margin), it returns exactly one chunk whose
// Content is byte-identical to the input.
func Chunk(content string, cfg ChunkConfig) []Chunk
```

Behavior:
- **Token counting** via `tiktoken-go`. Because its vocabulary does not match
  `nomic-embed-text`, target a safety fraction (e.g. `0.9 * MaxTokens`) so we
  never reach the point where Ollama truncates internally. The fraction is an
  internal constant, not a config field, unless review wants it configurable.
- **Structure-aware splitting** (content is markdown — the web app already
  renders it as markdown): prefer splitting on markdown headings, then
  blank-line paragraphs, then sentence boundaries, then a hard token-window
  fallback. The goal is coherent, self-contained chunks.
- **Overlap**: prepend `OverlapTokens` of the previous chunk's tail to each
  chunk after the first.
- **MaxChunks**: if the content would exceed `MaxChunks`, the final chunk
  absorbs the remainder (and we log/telegraph that the cap was hit). Never
  silently drop content.
- **Single-chunk fast path**: content under the limit returns one chunk
  identical to today's behavior — this is the "chunk only when over limit"
  decision.

No new embedding-provider behavior: each chunk is embedded with the existing
`embedder.Embed(ctx, chunk.Content)`.

### Component 3 — Multi-vector storage

Introduce a chunk table and move embeddings to per-chunk granularity.

New table (SQLite; Postgres mirror in `postgres.go`):

```sql
CREATE TABLE IF NOT EXISTS entry_chunks (
    rowid          INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id       TEXT    NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    chunk_index    INTEGER NOT NULL,
    content        TEXT    NOT NULL,
    token_estimate INTEGER NOT NULL DEFAULT 0,
    UNIQUE(entry_id, chunk_index)
);
```

Embeddings move from being keyed by `entries.rowid` to being keyed by
`entry_chunks.rowid`:
- SQLite-vec virtual table `vec_chunks` (`embedding FLOAT[dim]`) keyed by chunk
  rowid, replacing/augmenting `vec_entries`.
- Postgres/pgvector: chunk-keyed embeddings table mirroring the current one.

Storage API change:
- `StoreEntry(ctx, entry, embedding)` → `StoreEntry(ctx, entry, []Chunk, [][]float32)`
  (or a new `StoreEntryChunked` plus a thin shim that keeps the single-vector
  signature for callers that still pass one). Implementation writes the entry,
  then the chunk rows, then the chunk embeddings, in one transaction.
- `UpdateEntry` gains a chunk+embedding parameter so updates re-chunk/re-embed
  (see Component 5).

Search change (`internal/storage/sqlite.go` / `postgres_search.go`):
- Vector search runs over chunks → join chunk rowid → `entry_id`.
- **Dedupe to one row per entry, keeping the best-scoring chunk.** Return the
  entry; optionally carry the matched chunk's text as the result snippet.
- `knowledge_get`, `knowledge_list`, `knowledge_rate`, usage counting are
  unchanged — they operate on `entries` and never see chunks.

### Component 4 — Write path

`internal/mcp/tools.go` `HandleKnowledgeStore`:
1. Build the entry as today.
2. Resolve effective `ChunkConfig` for the team.
3. `chunks := Chunk(content, cfg)`.
4. Embed each chunk; abort with the existing "embedding not configured" error if
   no embedder.
5. `StoreEntry(ctx, entry, chunks, embeddings)`.
6. Response reports `chunk_count` and the effective limit, e.g.
   `"stored as 3 chunks (embedding_max_tokens=8192)"`, so the client gets
   explicit feedback instead of guessing about size.

### Component 5 — Update path (folds in an existing bug)

`internal/web/handlers.go` `handleKnowledgeUpdate` currently mutates
`existing.Content` and calls `UpdateEntry` **without re-embedding**, leaving the
stored vector stale. Fix as part of this work: on a content change, re-run the
chunker, re-embed, and replace the entry's chunk rows + embeddings atomically.

### Component 6 — Telegraphing limits in tool descriptions

Register a context-aware tool filter on the shared `*server.MCPServer`
(`mark3labs/mcp-go` v0.54.1 supports `WithToolFilter(func(ctx, []mcp.Tool) []mcp.Tool)`,
and `handleListTools` passes the request ctx). The filter:
- Reads `auth.GetTeamContext(ctx)` to get the team (HTTP transport).
- Resolves the team's effective `embedding_max_tokens`.
- Rewrites the `knowledge_store` (and optionally `knowledge_search`) description
  from a template, e.g.: *"Content of any length is accepted. Items larger than
  ~N tokens are automatically split into linked chunks internally and remain
  fully searchable as a single entry — do not pre-trim or split content
  yourself."*
- Over stdio (no auth context), falls back to the default-team / env effective
  value.

This keeps the registered descriptions as the source-of-truth template while
varying the concrete number per team.

## Data Flow

```
knowledge_store(content)
  -> resolve ChunkConfig (team -> env)
  -> Chunk(content, cfg)            [1..N chunks; 1 if under limit]
  -> for each chunk: embedder.Embed
  -> StoreEntry(entry, chunks, embeddings)   [entries + entry_chunks + vec_chunks]
  -> response: { id, chunk_count, embedding_max_tokens }

knowledge_search(query)
  -> embed(query)
  -> vector search over vec_chunks
  -> map chunk.rowid -> entry_id
  -> dedupe: best score per entry
  -> return entries (snippet = matched chunk)
```

## Error Handling

- No embedder configured → existing explicit error (unchanged).
- Partial embedding failure mid-item → whole `StoreEntry` transaction rolls back;
  return an error. No half-stored items.
- `MaxChunks` cap reached → never drop content; final chunk absorbs remainder and
  the response/telegraph notes the cap.
- `tiktoken-go` failure/unknown encoding → fall back to a char/4 heuristic so
  storage never hard-fails on token counting.

## Migration / Backward Compatibility

- Additive schema only (`entry_chunks`, `vec_chunks`, three `team_settings`
  columns) via idempotent `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE`.
- **Backfill**: for every existing entry, create `chunk_index 0` with
  `content = entries.content` and copy the existing embedding into the
  chunk-keyed vector table. Idempotent (guarded by `UNIQUE(entry_id, chunk_index)`
  / "skip if chunks already exist"). No re-embedding for data that already fit.
- Items under the limit behave identically to today.

## Testing

- `internal/embedding/chunk_test.go` — exact-boundary content, oversize content,
  unicode/multibyte, markdown structure (headings/paragraphs), overlap presence,
  `MaxChunks` remainder absorption, single-chunk fast-path identity.
- Storage round-trip: store a 3-chunk entry, confirm 3 chunk rows + 3 vectors;
  search returns the entry once (deduped) with the best-scoring chunk snippet.
- Migration/backfill test: pre-existing single-vector entry → one chunk row +
  copied vector; re-running backfill is a no-op.
- Update path test: content change re-chunks/re-embeds and replaces vectors.
- Tool-filter test: description reflects the team's effective `embedding_max_tokens`.

## Open Questions

- Should `vec_entries` be dropped after backfill, or kept until a later cleanup
  migration? (Lean: keep one release for safety, drop later.)
- Should the chunk safety fraction (`0.9`) be a config field or an internal
  constant? (Lean: internal constant unless review wants it exposed.)
