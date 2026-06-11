# Per-Touchpoint AI Configuration & Team-Aware Pipeline — Design

**Date:** 2026-06-11
**Status:** Approved

## Problem

1. **Bug:** the pipeline resolves LLM settings for `cfg.TeamID` (env `TEAM_ID`, default
   `"default"`), but real deployments have actual teams (this one has three). A team saving
   `llm_provider = ollama` saves it under their team UUID; the pipeline looks up the
   nonexistent `"default"` team, finds nothing, falls back to env, and uses Anthropic.
   Team-scoped pipeline queries (`CountEntries("default")` = 0) also make interval runs skip.
2. **Feature:** every LLM usage should be configurable by provider AND model, per team,
   per touchpoint — pick a team-level provider, then a model from that provider's
   available models.

## Decisions (user-approved)

| Decision | Choice |
|----------|--------|
| Pipeline | Per-team runs: iterate all teams, each processed with its own config + data scope; `TEAM_ID` no longer steers the pipeline |
| Touchpoints | Four: `analysis`, `agents`, `improvement`, `enrichment` |
| Fallback | Unset touchpoint → team default (`llm_provider` + role's legacy model) → env |
| Storage | One JSON `ai_touchpoints` column on team_settings |

## Touchpoint Map

| Touchpoint | Covers | Today's resolver |
|------------|--------|------------------|
| `analysis` | cluster summaries, quality scoring, gap detection | AnalysisLLM |
| `agents` | pipeline agent generation, web agent refactor | AgentLLM |
| `improvement` | weak-signal rewrites, auto-tagging (store-time + backfill) | ImprovementLLM (haiku pin) |
| `enrichment` | enrich_context, prompt_suggest | (piggybacked on AnalysisLLM — now split) |

Embeddings remain Ollama-only and out of scope.

## Config Model

`team_settings.ai_touchpoints` — TEXT (SQLite) / JSONB (Postgres), `NOT NULL DEFAULT '{}'`:

```json
{
  "analysis":    {"provider": "ollama",    "model": "llama3.1"},
  "improvement": {"provider": "anthropic", "model": "claude-haiku-4-5-20251001"}
}
```

- `storage.TeamSettings` gains `AITouchpoints map[string]storage.AITouchpoint` with
  `AITouchpoint{Provider, Model string}` (JSON round-trip through the column, `{}` default,
  unmarshal-failure → empty map).
- Valid touchpoint keys: the four above; valid providers: `anthropic` | `ollama`. PUT
  validation rejects unknown keys/providers (400).

## Resolution

`aiconfig`:

- `EffectiveConfig` gains `AITouchpoints map[string]storage.AITouchpoint` (saved values; no
  env layer for touchpoints — env only feeds the fallback chain below).
- New `Sources.LLMForTouchpoint(ctx, teamID, touchpoint string) llm.Client`:
  1. Touchpoint entry with `Provider` set →
     - `ollama`: `LLM.Ollama(OllamaURL.Effective, model)` where model = entry.Model, else
       team `OllamaLLMModel.Effective`.
     - `anthropic`: `LLM.Client(AnthropicAPIKey.Effective, model)` where model = entry.Model,
       else that touchpoint's anthropic fallback model (below).
  2. No entry / empty provider → existing `clientFor(cfg, anthropicFallbackModel)` behavior
     (team `llm_provider` switch, then env).
- Anthropic fallback models per touchpoint: `analysis`/`enrichment` →
  `AnthropicModel.Effective`; `agents` → `AgentModel.Effective`; `improvement` →
  `claude-haiku-4-5-20251001` (preserves current zero-config behavior exactly).
- `AnalysisLLM`/`AgentLLM`/`ImprovementLLM` become wrappers over `LLMForTouchpoint`; new
  `EnrichmentLLM(ctx, teamID)` added. `enrich_context.go` and `prompt_suggest.go` switch
  from `AnalysisLLM` to `EnrichmentLLM` (interface they consume: add the method or call the
  concrete Sources — match existing typing).
- `LLMFingerprint(ctx, teamID, touchpoint string)` gains the touchpoint parameter and
  reports the touchpoint's effective provider+model. Pipeline cache keys: score/summary use
  the `analysis` fingerprint; agent generations use the `agents` fingerprint.
- Nil-client and no-silent-fallback semantics unchanged.

## Team-Aware Pipeline

`internal/pipeline`:

- `Run(ctx, trigger)` becomes a per-team loop:
  1. `teams := store.ListTeams(ctx)`; if zero teams, run one unscoped pass with empty
     teamID (fresh-install/dev behavior preserved).
  2. For each team: resolve that team's clients via the touchpoint-aware Sources; gate on
     that team's `CountEntries` for interval runs; `StartPipelineRun(trigger, team.ID)`;
     process that team's entries (`ListEntries{TeamID}`); stamp clusters/agents/snapshots/
     runs with the team; run weak-signal + auto-tag backfill scoped to the team;
     `FinishPipelineRun`; prune cache once after the loop (not per team).
  3. A team whose processing fails records its own failed run and does NOT abort other
     teams' passes.
- `Pipeline.teamID` field and `WithWeakSignalImprovement(teamID)` lose the single-team
  meaning: `WithWeakSignalImprovement()` becomes a flag; the loop's team flows through
  parameters. `AnalysisStore` needs `ListTeams` (TeamStore already has it — extend the
  interface composition or add the method to AnalysisStore; both adapters already implement).
- `cmd/server/main.go`: pipeline construction drops `cfg.TeamID`; `TEAM_ID` remains only as
  `aiconfig.Sources.DefaultTeam` (stdio MCP fallback).
- Embedding/clustering inputs (`GetAllEmbeddings`) must scope per team — add a teamID
  parameter (`GetAllEmbeddings(ctx, teamID)`, empty = all) so clustering never mixes teams.

## Settings API & UI

- `GET /api/settings`: response gains `ai_touchpoints` (the saved map). `PUT` accepts it
  with validation (unknown touchpoint key or provider → 400). `import-env` untouched
  (touchpoints have no env layer).
- Settings page: new "AI Touchpoints" group in the AI card — one row per touchpoint with a
  provider `Select` (**Default / Anthropic / Ollama**) and a model `Select`/autocomplete
  populated from the chosen provider's available models (`fetchModelOptions()` already
  returns `anthropic` + `ollama` lists). "Default" clears the entry (inherit team default).
  Save flows through the existing full-payload save (include `ai_touchpoints` in state +
  payload — full-replace PUT semantics).

## Error Handling

- Unconfigured/unreachable provider for a touchpoint → nil client → that feature skips with
  the existing logged-warning behavior. No cross-provider fallback.
- Malformed `ai_touchpoints` JSON in the DB → treated as empty map (logged at warn in the
  storage scan).
- Per-team pipeline failures isolate to that team's run row.

## Non-Goals

- No per-user configuration; team-level only.
- No new providers.
- No embeddings changes.
- No retroactive re-analysis on config change beyond the existing provider-fingerprint
  cache invalidation.

## Testing

- aiconfig: LLMForTouchpoint table tests — explicit ollama entry (with/without model),
  explicit anthropic entry, unset → team default → env chain, per-touchpoint anthropic
  fallback models, EnrichmentLLM wired; fingerprint-per-touchpoint tests.
- storage: TeamSettings AITouchpoints JSON round-trip (both adapters compile, SQLite tested);
  malformed JSON → empty map; GetAllEmbeddings team scoping.
- pipeline: two teams with different fake configs → both processed independently, artifacts
  stamped per team, one team's failure doesn't abort the other, zero-teams fallback pass,
  interval gating per team.
- mcp: enrich_context/prompt_suggest use EnrichmentLLM (fake records which method).
- web: settings PUT round-trip + invalid touchpoint/provider 400; UI build clean.
- Full `go test ./...` + Vite build + throwaway-DB migration smoke.
