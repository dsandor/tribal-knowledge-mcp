# Per-Touchpoint AI Config & Team-Aware Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** Each of four AI touchpoints (analysis, agents, improvement, enrichment) is configurable per team with provider+model; the pipeline iterates teams so saved team settings actually apply (fixes the TEAM_ID="default" bug).

**Architecture:** `team_settings.ai_touchpoints` JSON map → `aiconfig.Sources.LLMForTouchpoint` resolution (touchpoint → team default → env; nil = skip, no fallback) with existing role methods as wrappers + new `EnrichmentLLM`. Pipeline `Run()` wraps its body in a per-team loop with per-team clients, gating, stamping, and run rows. Settings UI gets cascading provider→model pickers per touchpoint.

**Tech Stack:** Go dual adapters, React+MUI.

**Spec:** `docs/superpowers/specs/2026-06-11-ai-touchpoints-design.md`

**Key facts:**
- `TeamSettings`: `internal/storage/teams.go:54+`; Get/Put in `teams_sqlite.go` + `postgres_teams.go`; alters: `sqlite.go` teamSettingsAlters + postgres_teams DDL. JSON-column precedent: `Domains []string` round-trips through a TEXT column in those same functions — copy that marshal/unmarshal style.
- `aiconfig`: `aiconfig.go` (EffectiveConfig/resolve), `sources.go` (clientFor, AnalysisLLM/AgentLLM/ImprovementLLM, LLMFingerprint(ctx, teamID) — gains touchpoint param). Haiku pin const string appears in sources.go.
- MCP: `enrich_context.go:100` `src.AnalysisLLM(ctx, effectiveTeam)`; `prompt_suggest.go:50` `src.AnalysisLLM(ctx, teamID)` — both take `src *aiconfig.Sources` concretely. Other prompt_suggest LLM uses: check the whole file.
- Pipeline: `pipeline.go` — `AISource` interface (AnalysisLLM/AgentLLM/ImprovementLLM/LLMFingerprint), `Run()` resolves clients at :155-161, fingerprint once, `p.teamID` set via `WithWeakSignalImprovement(teamID)`; `runWeakSignalImprovement`/`runAutoTagBackfill` use `p.teamID`; `GetAllEmbeddings(ctx)` (analysis.go:~24, postgres_analysis.go) is UNSCOPED — gains teamID param. `ListTeams(ctx)` exists on both adapters (TeamStore); AnalysisStore must require it.
- main.go: pipeline built ~:223-230 with `.WithWeakSignalImprovement(cfg.TeamID)`.
- Settings API: `internal/web/ai_settings.go` (GET map, import-env — touchpoints NOT in import-env), `internal/web/settings.go` (PUT decode/validate/persist).
- Settings UI: `web/src/pages/Settings.tsx` (full-payload save — new state MUST be in payload), `api.ts` interfaces; `fetchModelOptions()` returns `{anthropic, ollama}` ModelOption lists.
- Test fixtures: pipeline `testhelpers_test.go` (mockAnalysisStore, mockAISource with fingerprint field, mockLLM call counter), aiconfig `sources_test.go` (fakeLLMProvider/fakeSettingsStore), web `ai_settings_test.go`, mcp `team_isolation_test.go` helpers.

---

### Task 1: Storage — AITouchpoint type, ai_touchpoints column, GetAllEmbeddings(teamID), ListTeams on AnalysisStore

**Files:**
- Modify: `internal/storage/teams.go`, `internal/storage/sqlite.go` (alters), `internal/storage/teams_sqlite.go`, `internal/storage/postgres_teams.go`, `internal/storage/storage.go` (AnalysisStore), `internal/storage/analysis.go` (GetAllEmbeddings), `internal/storage/postgres_analysis.go`
- Test: extend `internal/storage/teams_test.go` + `internal/storage/team_scoping_test.go`

- [ ] **Step 1: Failing tests.**
```go
// teams_test.go — TestTeamSettingsAITouchpointsRoundTrip:
// Put TeamSettings{..., AITouchpoints: map[string]AITouchpoint{
//   "analysis": {Provider: "ollama", Model: "llama3.1"}}}
// → Get → map round-trips exactly. Also: Put with nil map → Get returns empty
// (non-nil or nil consistently — pick non-nil empty map) without error.
//
// team_scoping_test.go — TestGetAllEmbeddingsTeamScoping:
// store two entries with embeddings (StoreEntry with a 4-dim vector — check how
// existing embedding tests build vectors) for teams t1/t2 →
// GetAllEmbeddings(ctx, "t1") returns only t1's entry id; GetAllEmbeddings(ctx, "")
// returns both.
```
Write real Go tests using existing fixtures (`newTestStoreInternal`; embedding dim is 4 in that helper).

- [ ] **Step 2:** Run them — compile FAIL.

- [ ] **Step 3: Implement.**
- `teams.go`:
```go
// AITouchpoint configures the provider and model for one AI touchpoint.
// Valid providers: "anthropic" | "ollama". Empty Model uses the provider's
// touchpoint default (see aiconfig.LLMForTouchpoint).
type AITouchpoint struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
```
  and on TeamSettings: `AITouchpoints map[string]AITouchpoint \`json:"ai_touchpoints"\`` — document the four valid keys (analysis, agents, improvement, enrichment).
- SQLite alter: `"ALTER TABLE team_settings ADD COLUMN ai_touchpoints TEXT NOT NULL DEFAULT '{}'"`; Postgres `ADD COLUMN IF NOT EXISTS ai_touchpoints TEXT NOT NULL DEFAULT '{}'` (TEXT is fine and matches Domains handling — check what type Domains uses in postgres and mirror).
- Get/PutTeamSettings both adapters: marshal/unmarshal the map exactly like Domains (unmarshal failure → `map[string]AITouchpoint{}` + continue).
- `storage.go` AnalysisStore: add `ListTeams(ctx context.Context) ([]Team, error)` (both adapters already implement — just declare).
- `GetAllEmbeddings(ctx, teamID string)`: SQLite + Postgres add the param; empty = all; non-empty joins/filters by entries.team_id (read the current query — it selects from entry_embeddings joined to entries or by rowid; add the JOIN/WHERE needed).
- Compiler sweep: `go build ./...` — update callers (pipeline Run passes its team — temporary `""` until Task 4 rewires; mocks in pipeline/web/mcp test files get the param + filter, and `ListTeams` already exists on web/mcp mocks? check — pipeline's mockAnalysisStore needs ListTeams added returning a settable slice).

- [ ] **Step 4:** `go build ./... && go test ./... 2>&1 | tail -5` — ALL pass.

---

### Task 2: aiconfig — AITouchpoints in EffectiveConfig, LLMForTouchpoint, EnrichmentLLM, touchpoint fingerprint

**Files:**
- Modify: `internal/aiconfig/aiconfig.go`, `internal/aiconfig/sources.go`
- Test: extend `internal/aiconfig/sources_test.go`

- [ ] **Step 1: Failing tests** (extend sources_test.go's fakeLLMProvider/fakeSettingsStore):
```go
// TestLLMForTouchpointExplicitOllama: saved AITouchpoints{"analysis": {Provider:"ollama", Model:"m1"}}
//   + OllamaURL set → Ollama called "url|m1"; Anthropic not called.
// TestLLMForTouchpointExplicitOllamaNoModel: entry {Provider:"ollama"} (no model) + team
//   OllamaLLMModel "chat1" → Ollama "url|chat1".
// TestLLMForTouchpointExplicitAnthropic: {"agents": {Provider:"anthropic", Model:"claude-z"}}
//   → Client "key|claude-z".
// TestLLMForTouchpointFallbackChain: no touchpoint entry + team llm_provider "" + env key →
//   Client called with the touchpoint's role-default model: analysis→AnthropicModel,
//   agents→AgentModel, improvement→haiku pin, enrichment→AnthropicModel (4 subtests).
// TestEnrichmentLLMUsesEnrichmentTouchpoint: {"enrichment": {Provider:"ollama", Model:"m2"}}
//   → EnrichmentLLM resolves Ollama "url|m2" while AnalysisLLM (unset) stays anthropic.
// TestLLMFingerprintPerTouchpoint: with the above config, LLMFingerprint(ctx,t,"enrichment")
//   == "ollama|url|m2" and LLMFingerprint(ctx,t,"analysis") == "anthropic|<analysis model>".
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.**
- `aiconfig.go`: `EffectiveConfig` gains `AITouchpoints map[string]storage.AITouchpoint \`json:"ai_touchpoints"\``; `Effective()` copies `saved.AITouchpoints` (nil → empty map). (No env layer; import `storage` is already there via SettingsStore.)
- `sources.go`:
```go
// Touchpoint names for per-usage AI configuration.
const (
	TouchpointAnalysis    = "analysis"
	TouchpointAgents      = "agents"
	TouchpointImprovement = "improvement"
	TouchpointEnrichment  = "enrichment"
)

const improvementHaikuModel = "claude-haiku-4-5-20251001"

// anthropicFallbackModel returns the Anthropic model used for a touchpoint when
// no explicit touchpoint model is configured.
func anthropicFallbackModel(cfg *EffectiveConfig, touchpoint string) string {
	switch touchpoint {
	case TouchpointAgents:
		return cfg.AgentModel.Effective
	case TouchpointImprovement:
		return improvementHaikuModel
	default: // analysis, enrichment
		return cfg.AnthropicModel.Effective
	}
}

// LLMForTouchpoint resolves the client for one AI touchpoint:
// explicit touchpoint entry → team default provider → env defaults.
// Returns nil when the resolved provider is unconfigured (callers skip+log).
func (s *Sources) LLMForTouchpoint(ctx context.Context, teamID, touchpoint string) llm.Client {
	if teamID == "" {
		teamID = s.DefaultTeam
	}
	cfg, err := s.Resolver.Effective(ctx, teamID)
	if err != nil {
		slog.Warn("aiconfig: resolve effective config", "touchpoint", touchpoint, "team", teamID, "err", err)
		return nil
	}
	if tp, ok := cfg.AITouchpoints[touchpoint]; ok && tp.Provider != "" {
		switch tp.Provider {
		case "ollama":
			model := tp.Model
			if model == "" {
				model = cfg.OllamaLLMModel.Effective
			}
			return s.LLM.Ollama(cfg.OllamaURL.Effective, model)
		case "anthropic":
			model := tp.Model
			if model == "" {
				model = anthropicFallbackModel(cfg, touchpoint)
			}
			return s.LLM.Client(cfg.AnthropicAPIKey.Effective, model)
		}
	}
	return s.clientFor(cfg, anthropicFallbackModel(cfg, touchpoint))
}

// EnrichmentLLM returns the client for prompt enrichment (enrich_context,
// prompt_suggest). See LLMForTouchpoint for resolution rules.
func (s *Sources) EnrichmentLLM(ctx context.Context, teamID string) llm.Client {
	return s.LLMForTouchpoint(ctx, teamID, TouchpointEnrichment)
}
```
- Rewrite AnalysisLLM/AgentLLM/ImprovementLLM bodies as `return s.LLMForTouchpoint(ctx, teamID, Touchpoint...)` (keep doc comments; the per-method resolve+warn collapses into LLMForTouchpoint — remove the now-duplicated bodies).
- `LLMFingerprint(ctx, teamID, touchpoint string)` — same resolution logic as LLMForTouchpoint but returning `"ollama|url|model"` / `"anthropic|model"` strings (factor a small shared `resolveTouchpoint(cfg, touchpoint) (provider, urlOrKeyIgnored, model)` helper if cleaner; the fingerprint must NOT include the API key — only `"anthropic|" + model`).
- Existing fingerprint callers updated in Task 4 (pipeline); for now `go build` will flag them — coordinate: in THIS task change the signature and mechanically update the pipeline call site to pass `aiconfig.TouchpointAnalysis`-equivalent via the AISource interface (the pipeline interface decl also needs the param — update `pipeline.go` AISource interface + testhelpers mockAISource signature mechanically; behavior refinement happens in Task 4).

- [ ] **Step 4:** `go build ./... && go test ./... 2>&1 | tail -5` — ALL pass.

---

### Task 3: MCP enrichment touchpoint

**Files:**
- Modify: `internal/mcp/enrich_context.go` (:100), `internal/mcp/prompt_suggest.go` (:50 + any other AnalysisLLM use in that file)
- Test: extend mcp tests

- [ ] **Step 1: Failing test.** In the mcp tests, the handlers take `src *aiconfig.Sources` — a real Sources with a fake LLMProvider + fake settings store (construct like aiconfig sources_test does; check what existing enrich/prompt_suggest tests use and extend). Contract:
```go
// TestEnrichmentTouchpointRouting: Sources whose saved settings set
// AITouchpoints{"enrichment": {Provider:"ollama", Model:"m2"}} with OllamaURL set,
// and anthropic env key present. Call the enrich_context handler (and one
// prompt_suggest handler) → the fake provider records an Ollama call (enrichment
// touchpoint honored), zero Anthropic calls from those handlers.
```
If existing tests construct Sources differently (e.g. via newSrc helpers), adapt — the assertion is what matters.

- [ ] **Step 2:** Run — FAIL (handlers still call AnalysisLLM → anthropic).

- [ ] **Step 3:** Change `src.AnalysisLLM(...)` → `src.EnrichmentLLM(...)` in both files (all enrichment-path call sites; read each file fully — prompt_suggest may resolve an LLM in more than one handler).

- [ ] **Step 4:** `go test ./internal/mcp/ 2>&1 | tail -2` + `go build ./...` — pass.

---

### Task 4: Team-aware pipeline

**Files:**
- Modify: `internal/pipeline/pipeline.go` (Run loop, AISource interface finalization, WithWeakSignalImprovement flag-ification), `internal/pipeline/cache.go` (fingerprint per touchpoint), `cmd/server/main.go`
- Test: `internal/pipeline/multiteam_test.go` (new) + adjust existing tests

- [ ] **Step 1: Failing tests** (reuse mockAnalysisStore/mockAISource/mockLLM; mockAnalysisStore needs `teams []storage.Team` + ListTeams):
```go
// TestRunIteratesTeams: store with teams t1,t2; entries+embeddings split between them.
// mockAISource returning distinct mockLLMs per team (extend the fake: map[teamID]llm.Client
// or record teamIDs passed to AnalysisLLM). After Run: AnalysisLLM was resolved for BOTH
// t1 and t2; clusters stamped per team (t1 clusters have TeamID t1 etc.); one pipeline
// run row per team.
// TestRunTeamFailureIsolated: t1's store interactions fail (e.g. fake returns error from
// ListEntries for t1 only — wrap the mock); t2 still completes with its own run row.
// TestRunZeroTeamsFallsBack: no teams → exactly one unscoped pass (one run row, teamID "").
// TestIntervalGatePerTeam is covered implicitly if gating moved inside the loop — assert
// a team below MinEntries is skipped (no run row) while the other runs. Check how the
// interval gate currently works (Start()'s ticker checks CountEntries before Run) — move
// the per-team gate INSIDE Run for interval triggers, or gate inside the loop; manual
// trigger processes all teams regardless (match current manual semantics).
```
Write real tests; adapt to the harness.

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.**
- `AISource` interface finalized: `AnalysisLLM/AgentLLM/ImprovementLLM(ctx, teamID)` unchanged; `LLMFingerprint(ctx, teamID, touchpoint string) string`. Pipeline passes touchpoint constants — define local consts or reuse plain strings "analysis"/"agents" (pipeline must not import aiconfig — keep plain strings, documented).
- `Run(ctx, trigger)` restructure: extract the current per-team body into `runForTeam(ctx, trigger, teamID string) error` (everything from StartPipelineRun through FinishPipelineRun, with ALL store calls scoped: ListEntries{TeamID}, GetAllEmbeddings(ctx, teamID), CountEntries(ctx, teamID) gate for interval triggers, clusters/snapshots/agents/runs stamped, weak-signal + autotag inside using teamID parameter instead of p.teamID). `Run` then: list teams → loop `runForTeam` per team (collecting but not propagating per-team errors; slog each) → zero teams → single `runForTeam(ctx, trigger, "")` → prune cache once after the loop.
- Score/summary cache calls use `p.src.LLMFingerprint(ctx, teamID, "analysis")`; agent generation uses `"agents"` fingerprint (thread both — resolve once per team).
- `WithWeakSignalImprovement(teamID)` → `WithWeakSignalImprovement()` (flag only); remove `p.teamID` field uses in favor of the loop's teamID parameter (the field may remain for nothing — delete it).
- `Start()` ticker: replace the global `CountEntries` pre-check with simply calling Run (per-team gates inside) — read the current code and keep the trigger-channel semantics.
- `main.go`: `.WithWeakSignalImprovement(cfg.TeamID)` → `.WithWeakSignalImprovement()`. `cfg.TeamID` keeps feeding `aiconfig.Sources.DefaultTeam` only.
- Update existing pipeline tests that constructed single-team runs (they seeded teams? previously no teams existed in mocks → they now hit the zero-teams fallback path which preserves old behavior — verify; fix expectations only where genuinely changed and SAY SO).

- [ ] **Step 4:** `go build ./... && go test ./... 2>&1 | tail -5` — ALL pass.

---

### Task 5: Settings API — ai_touchpoints GET/PUT + validation

**Files:**
- Modify: `internal/web/ai_settings.go` (GET ai block + saved map), `internal/web/settings.go` (PUT)
- Test: extend `internal/web/ai_settings_test.go`

- [ ] **Step 1: Failing tests:**
```go
// TestAISettingsIncludesTouchpoints: saved AITouchpoints{"analysis": {ollama, llama3.1}}
//   → GET ai block contains "ai_touchpoints" object with that entry.
// TestAISettingsPutTouchpoints: PUT body including
//   "ai_touchpoints": {"enrichment": {"provider": "ollama", "model": "m2"}}
//   → 200; persisted TeamSettings.AITouchpoints matches; re-GET round-trips.
// TestAISettingsPutTouchpointInvalidKey: key "embeddings" → 400, nothing persisted.
// TestAISettingsPutTouchpointInvalidProvider: provider "openai" → 400, nothing persisted.
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.** GET: add `"ai_touchpoints": eff.AITouchpoints` to the ai block (plain map — no FieldValue wrapper; it has no env layer). PUT (settings.go): decode `AITouchpoints map[string]storage.AITouchpoint \`json:"ai_touchpoints"\``; validate before persisting:
```go
	validTouchpoints := map[string]bool{"analysis": true, "agents": true, "improvement": true, "enrichment": true}
	for k, tp := range body.AITouchpoints {
		if !validTouchpoints[k] {
			writeError(w, 400, "bad_request", fmt.Sprintf("unknown ai touchpoint %q", k))
			return
		}
		if tp.Provider != "" && tp.Provider != "anthropic" && tp.Provider != "ollama" {
			writeError(w, 400, "bad_request", fmt.Sprintf("ai touchpoint %q: provider must be anthropic or ollama", k))
			return
		}
	}
```
Persist into TeamSettings (nil map → empty map). Import-env untouched.

- [ ] **Step 4:** `go test ./internal/web/ 2>&1 | tail -2` + build — pass.

---

### Task 6: Settings UI — cascading touchpoint pickers

**Files:**
- Modify: `web/src/lib/api.ts`, `web/src/pages/Settings.tsx`

- [ ] **Step 1: Implement.**
- `api.ts`: `export interface AITouchpoint { provider: string; model: string }`; Settings page's TeamSettings-ish interface + `AISettings` flow gains `ai_touchpoints?: Record<string, AITouchpoint>`.
- `Settings.tsx`: in the AI card, an "AI Touchpoints" subsection — for each of `[{key:'analysis',label:'Analysis (summaries, scoring, gaps)'},{key:'agents',label:'Agent generation & refactor'},{key:'improvement',label:'Improvement & auto-tagging'},{key:'enrichment',label:'Prompt enrichment (enrich_context, prompt_suggest)'}]` render a row: provider `Select` (Default ''/Anthropic/Ollama) + model picker enabled only when a provider is chosen, populated from `models.anthropic` or `models.ollama` per the selected provider (Autocomplete freeSolo matching the page's existing model fields). "Default" removes the key from the map. **State + save payload MUST carry `ai_touchpoints`** (full-replace PUT — same wipe rule as before; hydrate it from GET).
- Build: `cd /Users/dsandor/Projects/memory/web && npm run build` — clean.

---

### Task 7: Final verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all pass.
- [ ] **Step 2:** `cd web && npm run build` — clean.
- [ ] **Step 3:** Migration + behavior smoke on a THROWAWAY DB copy: copy `knowledge.db` → /tmp, open with NewSQLiteStore (768), `GetTeamSettings` for one of the real team IDs (list via ListTeams) — no error, AITouchpoints empty map; `GetAllEmbeddings(ctx, "")` works. Use the `./cmd/<tmp>` pattern from earlier plans (program inside the module; never touch the real DB; clean up).
- [ ] **Step 4:** Report. **Do not commit.** Note for the user: restart the server (`run.sh` uses `go run` from the working tree) for all of this to take effect, and the pipeline will now honor per-team settings without TEAM_ID.
