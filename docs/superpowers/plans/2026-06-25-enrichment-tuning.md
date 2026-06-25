# Per-user Enrichment Tuning + Relevance Threshold + Playground Implementation Plan

> **For agentic workers:** Implement task-by-task with TDD. **PROJECT POLICY: DO NOT COMMIT** — every task ends at a verification checkpoint (build + tests green); the owner commits. Never run `git commit/add/push`.
>
> **Commands:** Go build `go build ./...`; pkg tests `go test ./internal/<pkg>/`; web build (from `web/`) `npm run build`. Ignore macOS noise `deprecated|sqlite3.h|cgo-gcc`.

**Goal:** Stop prompt enrichment from including irrelevant memories — add a relevance threshold (sensible default for everyone), per-user enrichment preferences (threshold/max/LLM-rewrite/allow-deny domains+tags/pinned entries), and a web playground to test and tweak enrichment.

**Spec:** `docs/superpowers/specs/2026-06-25-enrichment-tuning-design.md`

**Architecture:** A `Relevance(distance)` helper normalizes vector distance to 0..1. A shared `SelectEnrichmentKnowledge` function (used by both the `enrich_context` MCP tool and a new preview endpoint) applies: team/visibility filters → deny → allow-list → relevance threshold (pins exempt) → sort (pins first) → cap. Per-user prefs live in `enrichment_prefs` (scalars) + `enrichment_rules` (allow/deny/pin lists), keyed by `EffectiveActorID`, with deployment defaults from env. A new `/enrichment` web page provides knob controls + a live playground.

**Defaults (approved):** `ENRICH_MIN_RELEVANCE=0.30`, `ENRICH_MAX_MEMORIES=5`; pins count toward the max.

---

## File Structure
- `internal/enrich/enrich.go` (new) — `Relevance`, `EnrichDefaults`, `EnrichmentPrefs`, `Candidate`, `SelectEnrichmentKnowledge` (the shared selection core). Keep this package free of web/mcp imports.
- `internal/config/config.go` — `EnrichMinRelevance`, `EnrichMaxMemories`.
- `internal/storage/teams.go` + `teams_sqlite.go` + `postgres_teams.go` + `sqlite.go`/`postgres_teams.go` (schema) — `EnrichmentPrefs` persistence types, tables, methods.
- `internal/mcp/enrich_context.go` — use `SelectEnrichmentKnowledge` + prefs.
- `internal/web/enrichment_handlers.go` (new) — prefs GET/PUT, preview, pin add/remove.
- `internal/web/server.go` — routes.
- `web/src/lib/api.ts`, `web/src/pages/Enrichment.tsx` (new), `web/src/components/Layout.tsx`, `web/src/App.tsx`, `web/src/pages/KnowledgeBrowser.tsx` — UI.

---

## Task 1: Relevance helper + EnrichDefaults

**Files:** create `internal/enrich/enrich.go`, `internal/enrich/enrich_test.go`.

- [ ] **Step 1: Failing test** (`enrich_test.go`, `package enrich`):
```go
package enrich

import "testing"

func TestRelevance(t *testing.T) {
	cases := []struct{ dist, want float64 }{
		{0, 1}, {0.7, 0.3}, {1, 0}, {1.5, 0}, {-0.1, 1},
	}
	for _, c := range cases {
		got := Relevance(c.dist)
		if got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("Relevance(%v)=%v want %v", c.dist, got, c.want)
		}
	}
}
```
- [ ] **Step 2: Run** `go test ./internal/enrich/` → FAIL (undefined).
- [ ] **Step 3: Implement** in `enrich.go`:
```go
// Package enrich holds the shared prompt-enrichment selection logic used by
// both the enrich_context MCP tool and the web preview endpoint.
package enrich

// Relevance maps a raw vector distance (lower = closer) to a 0..1 relevance
// (higher = better). For the cosine-distance embedders in use this approximates
// cosine similarity. Clamped to [0,1].
func Relevance(distance float64) float64 {
	r := 1 - distance
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// EnrichDefaults are the deployment-wide fallbacks for unset per-user prefs.
type EnrichDefaults struct {
	MinRelevance float64
	MaxMemories  int
}
```
- [ ] **Step 4: Run** `go test ./internal/enrich/` → PASS.
- [ ] **Step 5: Verify** `go build ./...`. Checkpoint (no commit).

---

## Task 2: Config env defaults

**Files:** `internal/config/config.go`, `internal/config/config_test.go`.

- [ ] **Step 1: Failing test** — extend `TestConfig_NewFields` (or add `TestConfig_EnrichDefaults`): set `t.Setenv("ENRICH_MIN_RELEVANCE","0.42")`, `t.Setenv("ENRICH_MAX_MEMORIES","7")`, `config.Load()`, assert `cfg.EnrichMinRelevance==0.42` and `cfg.EnrichMaxMemories==7`. Add a second test asserting defaults `0.30`/`5` when env unset.
- [ ] **Step 2: Run** `go test ./internal/config/` → FAIL (fields undefined).
- [ ] **Step 3: Implement** — add fields to the `Config` struct:
```go
EnrichMinRelevance float64
EnrichMaxMemories  int
```
and in `Load()` parse them with fallbacks (follow the existing `EMBEDDING_DIM` parse style at `config.go:81-90`):
```go
enrichMinRel := 0.30
if v := os.Getenv("ENRICH_MIN_RELEVANCE"); v != "" {
	if f, err := strconv.ParseFloat(v, 64); err == nil { enrichMinRel = f }
}
enrichMaxMem := 5
if v := os.Getenv("ENRICH_MAX_MEMORIES"); v != "" {
	if n, err := strconv.Atoi(v); err == nil && n > 0 { enrichMaxMem = n }
}
```
Assign into the returned `Config{... EnrichMinRelevance: enrichMinRel, EnrichMaxMemories: enrichMaxMem ...}`. (`strconv` is already imported.)
- [ ] **Step 4: Run** `go test ./internal/config/` → PASS.
- [ ] **Step 5: Verify** `go build ./...`. Checkpoint.

---

## Task 3: enrichment_prefs storage (tables + methods)

**Files:** `internal/storage/teams.go` (types + interface), `internal/storage/sqlite.go` + `postgres_teams.go` (schema), `internal/storage/teams_sqlite.go` + `postgres_teams.go` (methods), `internal/web/server_test.go` (mock stubs), `internal/storage/teams_test.go` (test).

- [ ] **Step 1: Types + interface** in `internal/storage/teams.go`:
```go
type EnrichmentPrefs struct {
	MinRelevance float64
	MaxMemories  int
	LLMRewrite   bool
	// Source flags: true when the scalar is a per-user override (not the default).
	MinRelevanceSet bool
	MaxMemoriesSet  bool
	LLMRewriteSet   bool
	AllowDomains []string
	DenyDomains  []string
	AllowTags    []string
	DenyTags     []string
	PinnedEntries []string
}
```
Add to `TeamStore` interface:
```go
// Enrichment preferences (per-user, keyed by EffectiveActorID). Scalars unset by
// the user are returned NOT-set so callers can apply deployment defaults.
GetEnrichmentPrefs(ctx context.Context, userID string) (*EnrichmentPrefs, error)
PutEnrichmentPrefs(ctx context.Context, userID string, minRel *float64, maxMem *int, llmRewrite *bool) error
ReplaceEnrichmentRules(ctx context.Context, userID, kind string, values []string) error
AddEnrichmentRule(ctx context.Context, userID, kind, value string) error
RemoveEnrichmentRule(ctx context.Context, userID, kind, value string) error
```
Valid `kind` values: `allow_domain, deny_domain, allow_tag, deny_tag, pin_entry`.

- [ ] **Step 2: Schema (both engines)** mirroring the `user_visibility_rules` pattern (find it in `sqlite.go` ~182 and `postgres_visibility.go`).
  - SQLite (`sqlite.go` phase5 tables):
    ```sql
    CREATE TABLE IF NOT EXISTS enrichment_prefs (
      user_id TEXT PRIMARY KEY, min_relevance REAL, max_memories INTEGER,
      llm_rewrite INTEGER, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
    CREATE TABLE IF NOT EXISTS enrichment_rules (
      user_id TEXT NOT NULL, kind TEXT NOT NULL, value TEXT NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      PRIMARY KEY (user_id, kind, value));
    ```
  - Postgres (`migrateTeams` or a `migrateVisibility`-style place): same with `TIMESTAMPTZ DEFAULT now()`.
- [ ] **Step 3: Failing test** (`teams_test.go`) `TestEnrichmentPrefs`:
  - Fresh store: `GetEnrichmentPrefs(ctx,"u1")` returns a prefs with the scalar *Set flags false and empty rule lists (no row yet).
  - `PutEnrichmentPrefs(ctx,"u1", ptr(0.5), ptr(3), ptr(false))`; Get → MinRelevance 0.5/Set true, MaxMemories 3/Set true, LLMRewrite false/Set true.
  - `ReplaceEnrichmentRules(ctx,"u1","deny_domain", []string{"legal","hr"})`; Get → DenyDomains == [legal hr]. `AddEnrichmentRule(...,"pin_entry","e1")`; Get → PinnedEntries==[e1]. `RemoveEnrichmentRule(...,"deny_domain","hr")`; Get → DenyDomains==[legal].
  (Add a small `ptr` helper in the test.)
- [ ] **Step 4: Run** `go test ./internal/storage/ -run TestEnrichmentPrefs` → FAIL (undefined).
- [ ] **Step 5: Implement methods (both engines).**
  - `GetEnrichmentPrefs`: SELECT the prefs row (nullable scalars → set the `*Set` flags by NULL-ness using `sql.NullFloat64/NullInt64`); SELECT all rule rows for the user and bucket by `kind` into the slices. Missing prefs row → all *Set false, zero scalars. Return resolved struct (do NOT apply defaults here — that's the caller's job, so the UI can show default-vs-override).
  - `PutEnrichmentPrefs`: UPSERT the row; a nil pointer arg writes SQL NULL (revert to default), a non-nil writes the value. Set `updated_at`.
  - `ReplaceEnrichmentRules`: in a tx, `DELETE FROM enrichment_rules WHERE user_id=? AND kind=?` then insert each value (lowercase domain/tag values; leave pin_entry raw). 
  - `AddEnrichmentRule`/`RemoveEnrichmentRule`: idempotent insert (`INSERT OR IGNORE`/`ON CONFLICT DO NOTHING`) / delete by (user,kind,value). Lowercase domain/tag values.
- [ ] **Step 6: Mock stubs** in `internal/web/server_test.go` (`mockStore`): add minimal implementations (an in-memory `enrichPrefs map[string]*storage.EnrichmentPrefs` is ideal so later web tests can drive it; at minimum return zero values). Run `go build ./... && go vet ./internal/...` to catch any cascade (pipeline/mcp mocks) and stub there too.
- [ ] **Step 7: Run** `go test ./internal/storage/ -run TestEnrichmentPrefs`; `go build ./...`; `go test ./internal/...` → PASS. Checkpoint.

---

## Task 4: SelectEnrichmentKnowledge (shared selection core)

**Files:** `internal/enrich/enrich.go` (add), `internal/enrich/enrich_test.go` (add). This package must not import `web`/`mcp`. It needs: a search function, embedder, chunk/visibility helpers. To keep it decoupled, define narrow dependency interfaces and pass already-fetched candidates where possible.

Design: keep `SelectEnrichmentKnowledge` PURE over a candidate list to make it trivially testable; the caller does the embedding + `SearchSimilar` + team/visibility filtering and hands in candidates.

- [ ] **Step 1: Types + failing test.** Add to `enrich.go`:
```go
import "github.com/dsandor/memory/internal/storage"

type Candidate struct {
	Entry     storage.KnowledgeEntry
	Relevance float64
	Included  bool
	Reason    string // "" if included; else below_threshold|denied|not_in_allowlist|over_max
	Pinned    bool
}

// ScoredEntry is the input: an entry plus its raw vector distance.
type ScoredEntry struct {
	Entry    storage.KnowledgeEntry
	Distance float64
}

// Select applies the per-user enrichment preferences to already-retrieved,
// already-team/visibility-filtered candidates. Returns included (kept, ordered)
// and excluded (dropped, with reason).
func Select(scored []ScoredEntry, prefs storage.EnrichmentPrefs) (included, excluded []Candidate)
```
Test `TestSelect` (`enrich_test.go`): construct entries with domains/tags and distances; prefs `{MinRelevance:0.3, MaxMemories:2}`; assert:
  - a distance-0.9 entry (relevance 0.1) is excluded with reason `below_threshold`;
  - a `DenyDomains:["legal"]` entry is excluded `denied` even if close;
  - with `AllowTags:["api"]`, a non-api entry is excluded `not_in_allowlist`;
  - a pinned entry with relevance below threshold is INCLUDED with `Pinned:true`;
  - when more than `MaxMemories` qualify, the lowest-relevance overflow is excluded `over_max`, pins kept first.
- [ ] **Step 2: Run** `go test ./internal/enrich/ -run TestSelect` → FAIL.
- [ ] **Step 3: Implement `Select`** following the spec pipeline order: deny → allow-list (pins exempt) → threshold (pins exempt) → sort (pinned first, then relevance desc) → cap to MaxMemories (overflow → over_max). Build `excluded` with the first reason that applies. Domain/tag comparisons are case-insensitive; tags check both `Entry.Tags` and `Entry.AutoTags`. Pinned = `Entry.ID ∈ prefs.PinnedEntries`.
- [ ] **Step 4: Run** `go test ./internal/enrich/` → PASS.
- [ ] **Step 5: Verify** `go build ./...`. Checkpoint.

---

## Task 5: Wire enrich_context to prefs + Select

**Files:** `internal/mcp/enrich_context.go`, and its construction site (where `RegisterEnrichContext` gets its deps — check `enrich_context.go:24` + caller in `internal/mcp/*` / `cmd/server/main.go`). Test: adapt `internal/mcp/enrich_context_test.go` if present, else add one.

- [ ] **Step 1:** Ensure the handler can reach `GetEnrichmentPrefs` and the `EnrichDefaults`. The handler already has a store + `src` (aiconfig). Add the deployment defaults: thread `enrich.EnrichDefaults{MinRelevance: cfg.EnrichMinRelevance, MaxMemories: cfg.EnrichMaxMemories}` into the MCP registration (follow how other config reaches MCP; if MCP construction doesn't currently take config, pass the two values through `RegisterEnrichContext`/the server struct). Resolve the caller id via `auth.GetTeamContext(ctx).EffectiveActorID()`.
- [ ] **Step 2:** Replace the inline knowledge block (`enrich_context.go:122-160`): 
  - `prefs, _ := store.GetEnrichmentPrefs(ctx, actorID)`; apply defaults: if `!prefs.MinRelevanceSet { prefs.MinRelevance = defaults.MinRelevance }`, same for MaxMemories; if `!prefs.LLMRewriteSet { prefs.LLMRewrite = true }`.
  - `fetchK := prefs.MaxMemories*3; if fetchK < 30 { fetchK = 30 }; if fetchK > 100 { fetchK = 100 }`.
  - embed + `SearchSimilar(vec, fetchK)` (existing), apply team-access + visibility filters (existing) to build `[]enrich.ScoredEntry{Entry, Distance: r.Score}`.
  - `included, _ := enrich.Select(scored, *prefs)`; map `included` → `relevant_knowledge` with `Score = c.Relevance`.
- [ ] **Step 3:** Gate the LLM rewrite (`enrich_context.go:171`) on `prefs.LLMRewrite`.
- [ ] **Step 4: Test** — add/adapt a test proving: with a default-threshold prefs and a fake search returning one close + one far entry, only the close one appears in `relevant_knowledge`, and its `Score` is the normalized relevance (≈1-distance). If the MCP handler is hard to unit-test directly, rely on `internal/enrich` unit tests (Task 4) for selection correctness and just verify build + existing MCP tests pass — but prefer a focused handler test if the harness allows. Report which.
- [ ] **Step 5: Verify** `go build ./...`; `go test ./internal/mcp/ ./internal/enrich/`. Checkpoint.

---

## Task 6: Web endpoints — prefs GET/PUT, preview, pins

**Files:** create `internal/web/enrichment_handlers.go`; `internal/web/server.go` (routes); test `internal/web/enrichment_handlers_test.go`.

- [ ] **Step 1: Handlers** (methods on `*Server`), authenticated (any role; keyed by `auth.GetTeamContext(r.Context()).EffectiveActorID()`):
  - `handleGetEnrichmentPrefs` (GET `/api/enrichment/prefs`): load prefs; apply defaults for unset scalars; return JSON `{min_relevance, max_memories, llm_rewrite, min_relevance_default, max_memories_default, defaults:{min_relevance,max_memories}, allow_domains, deny_domains, allow_tags, deny_tags, pinned_entries}` (the `_default` booleans = NOT the `*Set` flag, so the UI shows "using default").
  - `handlePutEnrichmentPrefs` (PUT): body `{min_relevance:*float, max_memories:*int, llm_rewrite:*bool, allow_domains:[], deny_domains:[], allow_tags:[], deny_tags:[], pinned_entries:[]}`. A null/omitted scalar → `PutEnrichmentPrefs` nil (revert to default). Replace each rule kind via `ReplaceEnrichmentRules`. Return the refreshed prefs (same shape as GET).
  - `handlePreviewEnrichment` (POST `/api/enrichment/preview`): body `{prompt, prefs_override?}`. Resolve prefs = override (if provided, with defaults applied to unset) else saved+defaults. Embed prompt via `s.aiSrc.Embedder` (if nil → still return rules + empty lists, no 500), `SearchSimilar(fetchK)`, team-access + visibility filter, build `[]enrich.ScoredEntry`, `enrich.Select`. Also compute applicable rules (reuse the same rule lookup `enrich_context` uses) and the improved prompt (reuse `buildEnhancedPrompt`; skip LLM rewrite in preview to keep it fast/deterministic — note this in the response, or honor llm_rewrite — keep it simple: preview does NOT call the LLM, returns the rule-enhanced prompt). Return `{included:[{id,title,domain,relevance,pinned}], excluded:[{id,title,domain,relevance,reason}], applicable_rules, improved_prompt}`.
  - `handlePinEntry` (POST `/api/enrichment/pins/{id}`) / `handleUnpinEntry` (DELETE): `AddEnrichmentRule`/`RemoveEnrichmentRule(actorID,"pin_entry",id)` → `{ok:true}`.
- [ ] **Step 2: Routes** in `internal/web/server.go` (authenticated, non-admin group with `authMW`+ActiveTeamMiddleware, alongside `/api/me`):
  ```go
  r.Get("/api/enrichment/prefs", s.handleGetEnrichmentPrefs)
  r.Put("/api/enrichment/prefs", s.handlePutEnrichmentPrefs)
  r.Post("/api/enrichment/preview", s.handlePreviewEnrichment)
  r.Post("/api/enrichment/pins/{id}", s.handlePinEntry)
  r.Delete("/api/enrichment/pins/{id}", s.handleUnpinEntry)
  ```
- [ ] **Step 3: Test** (`enrichment_handlers_test.go`): PUT prefs then GET round-trips (override + revert-to-default); pin add then GET shows it in `pinned_entries`, unpin removes; preview returns included/excluded with reasons for a seeded store + stub embedder (reuse `newReembedSources`/`mockStore` patterns; drive `mockStore.enrichPrefs` and `entries`). Auth: works for a normal member (personal data).
- [ ] **Step 4: Verify** `go test ./internal/web/`; `go build ./...`; from `web/` `npm run build` (api.ts unchanged yet — fine). Checkpoint.

---

## Task 7: Frontend API helpers

**Files:** `web/src/lib/api.ts`.

- [ ] Add (matching the file's existing GET/PUT/POST/DELETE helper style):
  - `getEnrichmentPrefs(): Promise<EnrichmentPrefs>` (GET `/api/enrichment/prefs`).
  - `putEnrichmentPrefs(p): Promise<EnrichmentPrefs>` (PUT).
  - `previewEnrichment(prompt: string, prefsOverride?): Promise<EnrichmentPreview>` (POST `/api/enrichment/preview`).
  - `pinEntry(id: string): Promise<void>` / `unpinEntry(id: string): Promise<void>`.
  Define local TS interfaces `EnrichmentPrefs` (min_relevance, max_memories, llm_rewrite, the `_default` booleans, defaults, allow/deny domains+tags, pinned_entries) and `EnrichmentPreview` (included[], excluded[], applicable_rules[], improved_prompt) matching the Task 6 JSON.
- [ ] **Verify:** from `web/` `npm run build`. Checkpoint.

---

## Task 8: Enrichment page (nav + controls + playground)

**Files:** create `web/src/pages/Enrichment.tsx`; `web/src/App.tsx` (route); `web/src/components/Layout.tsx` (nav item).

- [ ] **Step 1: Route + nav.** Add `<Route path="enrichment" element={<Enrichment />} />` in `App.tsx` (lazy/import like the other pages). Add a top-level nav entry `{ to: '/enrichment', label: 'Enrichment', Icon: <pick a lucide icon e.g. SlidersHorizontal> }` to the `baseNav` array in `Layout.tsx` (visible to all authenticated users).
- [ ] **Step 2: Page.** On mount `getEnrichmentPrefs()` into form state. Left panel controls:
  - Min relevance: a slider 0–100% (value = `min_relevance*100`); show "(default)" when `min_relevance_default`.
  - Max memories: number input; "(default)" hint when defaulted.
  - LLM rewrite: toggle.
  - Allow/Deny domains and tags: multi-selects. Populate options from existing data — fetch the domain/tag lists the app already exposes (check `api.ts` for a tags/domains list helper; if none, free-entry chips are acceptable).
  - Pinned entries: list with remove buttons; "add" via an entry-id/title search (reuse a knowledge search helper if available, else paste id).
  A **Save** button → `putEnrichmentPrefs(formState)` → refresh.
- [ ] **Step 3: Playground (right panel).** Prompt textarea + **Test** button → `previewEnrichment(prompt, formState)` (pass the CURRENT unsaved form values as `prefs_override` so users see the effect before saving). Render: **Included** memories (title, domain, relevance% badge, pin indicator); **Excluded** memories grouped/annotated by reason (below_threshold / denied / not_in_allowlist / over_max); the **applicable rules**; and the **improved prompt** (monospace block). Re-run preview automatically (debounced ~400ms) when a control changes, so tuning is live.
- [ ] **Step 4: Verify** from `web/` `npm run build`. Manually sanity-check the page renders, Test returns results, Save persists. Checkpoint.

---

## Task 9: "Pin to enrichment" action in the knowledge browser

**Files:** `web/src/pages/KnowledgeBrowser.tsx` (and/or `KnowledgeDetail.tsx`).

- [ ] Add a **"Pin to enrichment"** action on each entry (next to the existing Hide/mute affordance) → `pinEntry(e.ID)`; show a brief confirmation. If the entry is already pinned, offer **Unpin** → `unpinEntry(e.ID)`. (Knowing pinned-state may require including the caller's pins; simplest: optimistic toggle with a snackbar, since the authoritative list lives on the Enrichment page. Keep it minimal — a "Pin to enrichment" menu item that calls `pinEntry` and toasts "Pinned".)
- [ ] **Verify** from `web/` `npm run build`. Checkpoint.

---

## Final verification
- [ ] `go build ./...`, `go vet ./internal/...`, `go test ./internal/...` green; `gofmt -l` clean on touched files.
- [ ] `cd web && npm run build` succeeds.
- [ ] Manual smoke: with default prefs, `enrich_context`/preview drops weak matches; lower the slider and watch more appear; deny a domain and watch it disappear with reason; pin an entry from the browser and see it forced in; toggle LLM rewrite.
- [ ] Leave everything uncommitted.
