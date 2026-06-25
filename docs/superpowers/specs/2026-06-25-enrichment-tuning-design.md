# Per-user prompt-enrichment tuning + relevance threshold + playground — design

**Date:** 2026-06-25
**Status:** Approved (pending spec review)

## Problem

`enrich_context` (MCP) selects memories by pure vector kNN and returns the top 5 with **no relevance threshold** (`internal/mcp/enrich_context.go:128,146` — `wantK=5`, truncate-by-count; `Score` is a raw distance, surfaced but never used to filter). When the KB lacks 5 strongly-relevant entries, the remaining slots fill with weak matches — users get contextually irrelevant memories. The only per-user control today is the global, exclude-only "My Visibility" suppression (item/author/tag/domain), applied post-retrieval. There is no way to (a) drop weak matches, (b) per-user tune what enrichment pulls, or (c) test/preview enrichment.

## Decisions (locked)

- A real **relevance threshold**, with a sensible **deployment default applied to everyone** (`min_relevance=0.30`, `max_memories=5`) and **per-user override**.
- A **separate, enrichment-scoped** per-user preferences system (NOT folded into global "My Visibility", which stays as-is for hide-everywhere).
- Knobs: min relevance, max memories, LLM-rewrite on/off, allow/deny domains & tags, pinned entries.
- A **playground**: test a prompt, see included vs excluded memories (with reasons) and the improved prompt, tweak knobs live against unsaved overrides, then save.
- The Enrichment page is a **top-level left-nav** item. Pins are managed from **both** the knowledge browser (a "Pin to enrichment" action) and the Enrichment page.

## Components

### 1. Relevance normalization
`SearchResult.Score` is a raw distance (lower = better; `postgres.go:562`, `sqlite.go:1169`). Add a helper (in `internal/storage` or a small `internal/enrich` package):
```go
// Relevance maps a raw vector distance to a 0..1 relevance (higher = better).
// For the cosine-distance embedders in use this approximates cosine similarity.
func Relevance(distance float64) float64 { r := 1 - distance; if r < 0 { r = 0 }; if r > 1 { r = 1 }; return r }
```
The user-facing/threshold value is this relevance (shown as a %). The `enrichContextKnowledge.Score` field becomes this normalized relevance (document the change).

### 2. Deployment defaults
Config (env, with fallbacks) in `internal/config`:
- `ENRICH_MIN_RELEVANCE` (float, default `0.30`)
- `ENRICH_MAX_MEMORIES` (int, default `5`)
Surfaced to the selection layer as `EnrichDefaults{MinRelevance, MaxMemories}`. (No superadmin UI for these in v1 — env only.)

### 3. Per-user enrichment preferences (storage)
Keyed by `EffectiveActorID()` (user id → key id → "local"), mirroring visibility.

- Table `enrichment_prefs(user_id TEXT PK, min_relevance REAL NULL, max_memories INT NULL, llm_rewrite INT NULL, updated_at)`. NULL scalar = "use deployment default".
- Table `enrichment_rules(user_id TEXT, kind TEXT, value TEXT, created_at, PRIMARY KEY(user_id, kind, value))` where `kind ∈ {allow_domain, deny_domain, allow_tag, deny_tag, pin_entry}` (mirrors `user_visibility_rules`). `value` lowercased for domain/tag; raw entry id for `pin_entry`.
- Both engines (SQLite + Postgres), idempotent inserts (`INSERT OR IGNORE` / `ON CONFLICT DO NOTHING`).
- `TeamStore` (or a new `EnrichStore`) methods: `GetEnrichmentPrefs(ctx, userID) (*EnrichmentPrefs, error)` (returns a composite: resolved scalars + the rule lists; NULL scalars resolved against `EnrichDefaults`), `PutEnrichmentPrefs(ctx, userID, scalars)`, `AddEnrichmentRule(ctx, userID, kind, value)`, `RemoveEnrichmentRule(ctx, userID, kind, value)`.
- `EnrichmentPrefs` type carries: `MinRelevance float64`, `MaxMemories int`, `LLMRewrite bool` (all resolved), plus `AllowDomains/DenyDomains/AllowTags/DenyTags []string`, `PinnedEntries []string`, and `*Source` flags (whether each scalar is default or overridden, for the UI).

### 4. Shared selection function (single source of truth)
Extract the knowledge-selection logic from `HandleEnrichContext` into a reusable function so the live tool and the preview endpoint never diverge:
```go
type Candidate struct {
    Entry     storage.KnowledgeEntry
    Relevance float64
    Included  bool
    Reason    string // "" if included; else: below_threshold | denied | not_in_allowlist | over_max
    Pinned    bool
}
func SelectEnrichmentKnowledge(ctx, deps, prompt string, prefs EnrichmentPrefs) (included []Candidate, excluded []Candidate, err error)
```
Pipeline (order matters):
1. Embed prompt; `SearchSimilar(vec, fetchK)` with `fetchK = max(prefs.MaxMemories*3, 30)` capped at 100 (raise the current fetch so the threshold has candidates to work with).
2. Team-access filter (existing `CanAccess`) + global visibility filter (existing) — unchanged.
3. **Deny**: drop entries whose domain ∈ DenyDomains or any tag ∈ DenyTags → reason `denied`.
4. **Allow-list**: if AllowDomains or AllowTags non-empty, keep only entries matching one (pins exempt) → others reason `not_in_allowlist`.
5. Compute `Relevance`; **threshold**: drop `Relevance < MinRelevance` → reason `below_threshold`. **Pinned entries that surfaced are exempt** (always included), `Pinned=true`.
6. Sort: pinned first, then by relevance desc. Truncate to `MaxMemories`; the overflow → reason `over_max`. (Pins count toward the cap but are prioritized.)
`included` = the kept set; `excluded` = everything dropped with its reason (for the playground).

### 5. `enrich_context` (MCP) changes
- Resolve `prefs := GetEnrichmentPrefs(ctx, EffectiveActorID)`.
- Replace the inline selection (`enrich_context.go:122-160`) with `SelectEnrichmentKnowledge(...)`; map `included` → `relevant_knowledge` with `Score = Relevance`.
- Honor `prefs.LLMRewrite` (skip the LLM rewrite step when off).
- Graceful-degradation behavior preserved (errors → partial result).

### 6. Endpoints
Authenticated (any role; personal data keyed by the caller):
- `GET /api/enrichment/prefs` → resolved prefs + rule lists + which scalars are default vs overridden.
- `PUT /api/enrichment/prefs` → set scalars (min_relevance/max_memories/llm_rewrite; null/omitted = revert to default) and replace the rule lists (allow/deny domains+tags, pins) — or use granular add/remove; choose one and keep the API coherent. (Plan: PUT replaces scalars + lists in one call; plus `POST /api/enrichment/pins/{entryId}` and `DELETE .../{entryId}` for the browser "Pin" action.)
- `POST /api/enrichment/preview` `{prompt, prefs_override?}` → runs `SelectEnrichmentKnowledge` with the override (or saved) prefs and returns `{included:[{id,title,domain,relevance,pinned}], excluded:[{id,title,domain,relevance,reason}], applicable_rules, improved_prompt}`. Does NOT persist. This powers the live playground.

### 7. Web UI
- New top-level left-nav item **Enrichment** (`/enrichment`), visible to all authenticated users (personal page).
- Layout: left = tuning controls (min-relevance slider shown as %, max-memories number, LLM-rewrite toggle, allow/deny domain & tag multi-selects populated from existing domains/tags, pinned-entries list with add-by-search + remove); right = playground (prompt textarea + Test button → included memories list with relevance % badges, excluded list grouped/annotated by reason, applicable rules, and the improved prompt). Knob changes re-run `POST /api/enrichment/preview` (debounced) with the *unsaved* current control values so the user sees the effect live. A **Save** button persists via `PUT /api/enrichment/prefs`; an indicator shows default vs overridden values.
- Knowledge browser (`KnowledgeBrowser.tsx`): add a **"Pin to enrichment"** row/action (next to the existing Hide action) → `POST /api/enrichment/pins/{id}`; reflect pinned state.
- api.ts helpers: `getEnrichmentPrefs`, `putEnrichmentPrefs`, `previewEnrichment`, `pinEntry`, `unpinEntry`.

## Testing
- `Relevance` mapping (clamps; distance 0→1.0, 0.7→0.3, ≥1→0).
- Storage: prefs round-trip with NULL→default resolution; rules add/remove/list; both engines.
- `SelectEnrichmentKnowledge`: threshold drops weak matches (reason `below_threshold`); deny drops; allow-list restricts; pins bypass threshold and sort first; `over_max` truncation; excluded set carries correct reasons. (Unit test with a fake search returning known distances.)
- `enrich_context`: weak matches now excluded; `Score` is normalized relevance; `llm_rewrite=off` skips rewrite. (Adapt existing enrich tests.)
- Endpoints: prefs GET/PUT round-trip; preview returns included/excluded with reasons; pin add/remove; auth.
- Web build.

## Defaults & migration notes
- Existing deployments: the default `min_relevance=0.30`/`max=5` applies immediately (behavior change — fewer, more-relevant memories). Tunable via env if 0.30 is too aggressive/lax for a given corpus; the playground lets users/ops calibrate against real data.
- No re-indexing required (this is a read-path/selection change only).
