# Server Observability: MCP, AI, and Pipeline Logging — Design

**Date:** 2026-06-11
**Status:** Approved

## Problem

The console shows only HTTP request logs (`slogRequestLogger`, internal/web/server.go:314).
MCP tool calls are invisible; the AI layer never reports which provider/model it resolved and
LLM failures inside pipeline stages land in the run row without reaching the console; the
pipeline logs skips but not lifecycle or stage progress. One message predates provider choice
("pipeline skipped: no effective anthropic key").

## Decision (user-approved)

Instrument at three seams with uniform slog fields; default `info` shows MCP activity, AI
failures/unconfigured warnings, and pipeline lifecycle; `debug` adds every successful LLM
call and cache hits/misses. Same slog JSON/stderr stream; no new dependencies.

## Components

### 1. LLM logging wrapper (`internal/llm/logging.go`)

```go
// LoggingClient wraps a Client with structured logging. Errors log at Warn
// with full context; successes log at Debug with duration.
type LoggingClient struct {
	Inner llm.Client          // (in-package: Client)
	Attrs []slog.Attr         // provider, model, touchpoint, team
}
func (c *LoggingClient) Complete(ctx context.Context, prompt string) (string, error)
```

- Error: `slog.Warn("llm call failed", attrs..., "duration_ms", d, "err", err)`.
- Success: `slog.Debug("llm call", attrs..., "duration_ms", d)`.
- Applied in `aiconfig.Sources.LLMForTouchpoint`: wrap the resolved client with
  provider/model/touchpoint/team attrs before returning. When resolution yields nil,
  `slog.Warn("llm unconfigured", "touchpoint", t, "team", id, "provider", p)` once per call
  (callers already skip — this makes the skip visible).
- The wrapper must preserve nil semantics (nil inner → return nil, not a wrapped nil) and
  keep `llm.Provider` cache behavior intact (wrap OUTSIDE the cache so cached identity is
  unchanged; the wrapper itself is cheap per-call construction in Sources).
- CAUTION: `Sources.clientFor`/`LLMForTouchpoint` returns are compared against nil by every
  caller — a typed-nil pointer inside the interface would break those checks; return the
  untyped nil explicitly.

### 2. MCP tool decorator (`internal/mcp/logging.go`)

```go
// logTool wraps an MCP tool handler with invocation logging.
func logTool(name string, h server.ToolHandlerFunc) server.ToolHandlerFunc
```

- Logs `slog.Info("mcp tool", "tool", name, "team", teamID, "duration_ms", d, "status", s)`
  where status is `ok`, `error` (handler returned a tool-error result — use result.IsError),
  or `panic`-free protocol error (err != nil → also `err` field).
- teamID via `resolveActorTeam(ctx)` (empty for stdio — logged as "").
- Applied at EVERY tool registration site (`NewMCPServer`, RegisterAnalysisTools,
  RegisterAgentTools, RegisterRuleTools, RegisterKnowledgeExtTools, prompt_suggest/
  enrich_context registrations — grep `AddTool(` to enumerate; all wrapped).
- Resources and prompts are NOT wrapped (low traffic; YAGNI).

### 3. Pipeline lifecycle & stage logging (`internal/pipeline/pipeline.go`)

At info:
- Run start (per team): `slog.Info("pipeline run started", "team", id, "trigger", t,
  "entries", n, "analysis_llm", fpA, "agents_llm", fpG, "improvement_llm", fpI)` — the
  fingerprints already exist (`LLMFingerprint`); resolve the improvement one alongside.
- Run finish: `slog.Info("pipeline run finished", "team", id, "status", s, "duration_ms",
  d, "clusters", c, "errors", len(errs))`.
- Stage failures: each place a stage error is appended to runErrs (or logged-and-continued)
  gains/keeps a `slog.Warn` with `"stage"` (cluster|summarize|score|gaps|agent_gen|
  weak_signal|autotag), `"team"`, `"err"`. Existing logs are normalized to include team.
- Fix stale wording: "pipeline skipped: no effective anthropic key" → `"pipeline skipped:
  no LLM configured"` with `"team"` (provider-aware).

At debug:
- Analysis cache hit/miss per lookup: `slog.Debug("analysis cache", "kind", k, "hit", b,
  "team", id)` in cache.go helpers.

## Non-Goals

- No metrics/tracing/OTel, no log files, no UI log viewer, no MCP resource/prompt logging.
- No change to the HTTP request logger.

## Testing

- `internal/llm`: LoggingClient passthrough (result/err identical), Warn on error and Debug
  on success captured via a test slog handler; nil-inner → nil.
- `internal/aiconfig`: wrapped client still satisfies nil checks when unconfigured;
  "llm unconfigured" warn fires (test handler).
- `internal/mcp`: logTool logs tool/status/duration for ok and IsError results; handlers'
  results pass through unchanged.
- `internal/pipeline`: run start/finish logs carry team + fingerprints (test handler);
  stage-failure warn on a forced scoring error.
- Full `go test ./...`; no frontend changes (build check only).
