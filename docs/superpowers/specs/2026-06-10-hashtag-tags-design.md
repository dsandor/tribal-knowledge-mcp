# Hashtag & Auto-Tag Support — Design

**Date:** 2026-06-10
**Status:** Approved

## Problem

Knowledge entries carry a single `Tags []string` field with no provenance. Users cannot tag
entries by writing `#hashtags` naturally in their prompts, the system never auto-categorizes
entries, and the UI barely surfaces tags (none on the Knowledge Browser; plain outlined chips
on the Detail page). Analysts need to see at a glance which tags a person chose and which the
system inferred, and to navigate by tag.

## Goals

1. Extract `#hashtags` from incoming title/content at store time and merge them with any
   explicit tags array (non-destructive — text is unchanged).
2. Auto-categorize entries with LLM-generated tags, asynchronously, without blocking the
   store path; backfill the existing corpus via a pipeline stage.
3. Render user tags and auto tags as visually distinct pills in the Knowledge Browser and
   Knowledge Detail views.
4. Make pills clickable to filter the Knowledge Browser by tag (SQL-level filter).

## Non-Goals

- Tag analytics, tag merge/rename, or curator tag management (future).
- Including `auto_tags` in FTS indexes (tag filtering is exact-match; FTS unchanged).
- Editing auto tags in the UI (read-only in v1).

## Data Model

Add a parallel `auto_tags` column next to the existing `tags` column.

| Adapter | Column | Type | Default |
|---------|--------|------|---------|
| SQLite (`internal/storage/sqlite.go`) | `auto_tags` | TEXT (JSON array) | `'[]'` |
| PostgreSQL (`internal/storage/postgres.go`) | `auto_tags` | JSONB | `'[]'` |

- `KnowledgeEntry` (`internal/storage/storage.go:24`) gains `AutoTags []string`.
- Migration uses the existing idempotent `ALTER TABLE ... ADD COLUMN` pattern in both
  adapters' `migrate()` methods.
- Read/write mirrors the existing `tags` JSON marshal/unmarshal, falling back to `[]string{}`
  on unmarshal error.
- New store method on the `Store` interface: `UpdateAutoTags(ctx, id string, tags []string) error`
  (updates `auto_tags` and `updated_at` only; does not bump `version`).

**Alternatives rejected:** tag objects `[{name, source}]` in one column (breaks every existing
reader: MCP output, CSV import/export, UI edit field, FTS triggers); normalized tags table
(~10 new store methods across two adapters for no v1 capability — YAGNI).

## Hashtag Extraction

New package `internal/tags`:

```go
// ExtractHashtags returns lowercase, deduplicated tags for every #hashtag in text.
// Pattern: #([A-Za-z0-9][A-Za-z0-9_-]*) — must start alphanumeric, allows _ and -.
func ExtractHashtags(text string) []string

// Merge combines explicit tags and extracted hashtags, lowercasing and deduplicating
// while preserving first-seen order.
func Merge(explicit, extracted []string) []string
```

Call sites (store time, before `StoreEntry`):

- MCP `knowledge_store` handler (`internal/mcp/tools.go:23`) — extract from title + content,
  merge with the `tags` argument. Tool description updated to mention inline `#hashtags`.
- Web create handler (`internal/web/handlers.go` store endpoint) — same merge.
- Import handlers (`internal/web/import_handlers.go`, JSON + CSV paths) — same merge.

Hashtags are **not** stripped from stored content.

## Auto-Categorization

- After a successful store (MCP and web create paths), spawn a goroutine:
  1. Resolve an LLM client via the existing `aiconfig.Sources` pattern, pinned to
     `claude-haiku-4-5-20251001` (same approach as `ImprovementLLM`).
  2. Prompt: given title/content/type/domain, return ONLY JSON
     `{"tags": ["...", ...]}` with 3–5 short lowercase category tags.
  3. Dedupe case-insensitively against the entry's user `Tags`.
  4. Persist via `UpdateAutoTags`. Any error (LLM, parse, store) is logged at warn and
     dropped — the entry simply has no auto tags.
- The goroutine carries a bounded timeout (30s) and uses a context detached from the request.
- Encapsulated in `internal/tags/autotag.go`: `AutoTagger{Store, Sources}.TagEntry(ctx, entry)`
  so MCP and web handlers share one implementation and tests can fake the LLM.

**Backfill:** new pipeline stage `AutoTagBackfill` (in `internal/pipeline`), gated like other
optional stages. It lists entries with empty `auto_tags` and runs the same `AutoTagger` over
them (bounded batch per run). Idempotent: already-tagged entries are skipped, so it acts as a
one-time backfill that also catches any entries whose async tagging failed.

## API

- `AutoTags` flows into all existing JSON responses automatically (whole-struct marshal).
- `ListFilter` (`internal/storage/storage.go:48`) gains `Tag string`.
- SQL-level filter matching **either** column:
  - SQLite: `EXISTS (SELECT 1 FROM json_each(entries.tags) WHERE value = ?) OR
    EXISTS (SELECT 1 FROM json_each(entries.auto_tags) WHERE value = ?)`
  - Postgres: `jsonb_exists(tags, $n) OR jsonb_exists(auto_tags, $n)`
- `GET /api/knowledge` accepts `tag=` and maps it to `ListFilter.Tag`.
- Export handler's post-hoc tag loop is replaced by `ListFilter.Tag` (matches user tags and
  auto tags; CSV export gains an `auto_tags` column, pipe-separated like `tags`).

## UI

**`TagPill` component** (`web/src/components/ui/tag-pill.tsx`):

- `variant="user"`: filled chip, emerald tint (`#10b981` family on dark theme), label `#tag`.
- `variant="auto"`: outlined chip, indigo (`#6366f1`), `AutoAwesome` icon at small size.
- Optional `onClick` → cursor pointer; tooltip on auto pills: "Auto-categorized".

**Knowledge Browser** (`web/src/pages/KnowledgeBrowser.tsx`):

- Cards render user pills then auto pills, capped at 5 total with a `+N` overflow chip.
- Clicking a pill sets a tag filter; an active filter renders a removable chip in the toolbar.
- `api.ts` `listKnowledge` gains a `tag` parameter; `KnowledgeEntry` interface gains
  `AutoTags: string[] | null | undefined`.

**Knowledge Detail** (`web/src/pages/KnowledgeDetail.tsx`):

- View mode: existing tag chips replaced by `TagPill` rows (user + auto inline).
- Edit mode: comma-separated field continues to edit user tags only; auto pills shown
  read-only beneath it.

## Error Handling

- LLM/auto-tag failures never surface to the storing client; logged via slog at warn.
- Empty/whitespace hashtag candidates and duplicates (case-insensitive) are dropped.
- `tag=` filter with no matches returns an empty list (200), consistent with other filters.

## Testing

- `internal/tags`: extraction table tests (unicode, punctuation boundaries, `#` mid-word,
  dedup, case), merge tests, AutoTagger test with a fake LLM (success, malformed JSON, error).
- Storage: SQLite round-trip of `AutoTags`, `UpdateAutoTags`, `ListFilter.Tag` matching each
  column and neither.
- Web: handler test for `GET /api/knowledge?tag=...`; export filter test.
- MCP: `knowledge_store` merges inline hashtags with the explicit array.
- Frontend: clean `vite build`; manual verification of both pill variants and click-to-filter.

## Decisions Log

| Decision | Choice |
|----------|--------|
| User tag source | Extract `#hashtags` from text + explicit array, non-destructive |
| Auto-tag timing | Async at store time; pipeline stage backfills existing corpus |
| Visual distinction | User: filled emerald `#tag`; Auto: outlined indigo + ✦ icon |
| Tag navigation | Click-to-filter with `tag` API param |
| Storage | Parallel `auto_tags` column (JSON/JSONB) |
