# Server Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PROJECT RULE — NO GIT COMMITS.** Never run `git add`/`git commit`. Every task ends at "tests pass".

**Goal:** The console reports MCP tool activity, AI usage/failures (provider+model+touchpoint), and pipeline lifecycle — not just HTTP calls.

**Architecture:** Three seams: `llm.LoggingClient` applied inside `Sources.LLMForTouchpoint` (knows provider/model/touchpoint/team); a `logTool` decorator at every MCP tool registration; explicit lifecycle/stage logs in the pipeline. Default info = activity + failures; debug = per-call LLM + cache hits.

**Spec:** `docs/superpowers/specs/2026-06-11-observability-design.md` — READ IT FULLY; the field names and levels there are authoritative.

**Key facts:**
- slog: JSON → stderr, level from LOG_LEVEL (`cmd/server/main.go:59-70`). HTTP logger: `internal/web/server.go:314-330` (untouched).
- `llm.Client` interface: `internal/llm/client.go`. `Sources.LLMForTouchpoint`/`clientFor`: `internal/aiconfig/sources.go` (returns are nil-checked by ALL callers — typed-nil hazard, see spec CAUTION).
- MCP registrations: grep `AddTool(` across `internal/mcp/` (server.go NewMCPServer + RegisterAnalysisTools/RegisterAgentTools/RegisterRuleTools/RegisterKnowledgeExtTools + any prompt_suggest/enrich registrations). Handler type: `server.ToolHandlerFunc` = `func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error)`; tool-level errors are `result.IsError`. `resolveActorTeam(ctx)`: live_producer.go:15.
- Pipeline: `runForTeam` resolves clients + two fingerprints near its top; FinishPipelineRun marks status; stage errors appended to `runErrs` at several sites (grep `runErrs = append`); existing slog lines to normalize with "team". Stale message at pipeline.go:232. Cache helpers: `internal/pipeline/cache.go`.
- Test-handler pattern for asserting logs: `slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))` + `slog.SetDefault` (restore prior default via t.Cleanup) — check whether any existing test already captures slog and reuse its style.

---

### Task 1: `llm.LoggingClient` + Sources wiring

**Files:**
- Create: `internal/llm/logging.go`, `internal/llm/logging_test.go`
- Modify: `internal/aiconfig/sources.go`
- Test: extend `internal/aiconfig/sources_test.go`

- [ ] **Step 1: Failing tests.**

`internal/llm/logging_test.go`:
```go
// TestLoggingClientPassthrough: inner returns ("hi", nil) → wrapper returns identical;
// captured log contains level=DEBUG msg="llm call" and the attrs given.
// TestLoggingClientErrorWarns: inner errors → identical error returned; captured log
// contains level=WARN msg="llm call failed" with err and duration_ms.
```
`internal/aiconfig/sources_test.go`:
```go
// TestLLMForTouchpointWrapsWithLogging: resolved client is NOT the fake provider's raw
// client (it's wrapped) but Complete passes through to it (call it; fake records).
// TestLLMUnconfiguredWarns: ollama selected but no model → returns UNTYPED nil
// (c == nil must be true for the llm.Client interface value!) and captured log contains
// "llm unconfigured" with touchpoint+team.
```
Write real Go tests with a buffer-backed slog handler (set default, restore in cleanup).

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.**

`internal/llm/logging.go`:
```go
package llm

import (
	"context"
	"log/slog"
	"time"
)

// LoggingClient wraps a Client with structured logging: failures at Warn with
// full provider context, successes at Debug with duration.
type LoggingClient struct {
	Inner Client
	Attrs []any // alternating key/value pairs: provider, model, touchpoint, team
}

func (c *LoggingClient) Complete(ctx context.Context, prompt string) (string, error) {
	start := time.Now()
	out, err := c.Inner.Complete(ctx, prompt)
	d := time.Since(start).Milliseconds()
	if err != nil {
		args := append(append([]any{}, c.Attrs...), "duration_ms", d, "err", err)
		slog.Warn("llm call failed", args...)
		return out, err
	}
	args := append(append([]any{}, c.Attrs...), "duration_ms", d)
	slog.Debug("llm call", args...)
	return out, nil
}
```

`sources.go` — in `LLMForTouchpoint`, factor the final client construction so every non-nil return is wrapped, and every nil return logs:
```go
// wrapLogged wraps a resolved client with logging context, preserving untyped
// nil (a typed-nil inside the interface would defeat callers' nil checks).
func wrapLogged(c llm.Client, provider, model, touchpoint, teamID string) llm.Client {
	if c == nil {
		slog.Warn("llm unconfigured", "touchpoint", touchpoint, "team", teamID, "provider", provider, "model", model)
		return nil
	}
	return &llm.LoggingClient{Inner: c, Attrs: []any{"provider", provider, "model", model, "touchpoint", touchpoint, "team", teamID}}
}
```
Restructure LLMForTouchpoint's branches to compute (provider, model, rawClient) then `return wrapLogged(...)` — including the clientFor fallback path (clientFor needs the same treatment or LLMForTouchpoint post-wraps its result knowing the fallback's provider/model; factor a `resolveTouchpoint(cfg, touchpoint) (provider, model string)` helper shared with LLMFingerprint if that simplifies — the reviewer previously recommended exactly that deduplication, do it now).

CAUTION: `LLMFingerprint` must keep returning the same strings (tests pin them).

- [ ] **Step 4:** `go build ./... && go test ./internal/llm/ ./internal/aiconfig/ ./internal/pipeline/ ./internal/mcp/ 2>&1 | tail -4` — ALL pass (pipeline/mcp fakes implement AISource/LLMProvider — unaffected by wrapping since it's inside real Sources only; verify).

---

### Task 2: MCP tool decorator

**Files:**
- Create: `internal/mcp/logging.go`, `internal/mcp/logging_test.go`
- Modify: every `AddTool(` site in `internal/mcp/` (grep; server.go, analysis_tools.go, agent_tools.go, rule_tools.go, knowledge_tools.go, prompt_suggest.go/enrich_context.go registrations)

- [ ] **Step 1: Failing tests** (`logging_test.go`, buffer-backed handler):
```go
// TestLogToolOK: wrap a handler returning NewToolResultText → result passes through
// unchanged; log line has msg="mcp tool", tool="x", status="ok", duration_ms present.
// TestLogToolError: handler returns NewToolResultError → status="error".
// TestLogToolProtocolError: handler returns (nil, errors.New("boom")) → status="error",
// err field present, error propagated unchanged.
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement** `internal/mcp/logging.go`:
```go
package mcp

import (
	"context"
	"log/slog"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// logTool wraps an MCP tool handler with uniform invocation logging.
func logTool(name string, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		start := time.Now()
		teamID, _ := resolveActorTeam(ctx)
		result, err := h(ctx, req)
		d := time.Since(start).Milliseconds()
		status := "ok"
		if err != nil || (result != nil && result.IsError) {
			status = "error"
		}
		if err != nil {
			slog.Info("mcp tool", "tool", name, "team", teamID, "duration_ms", d, "status", status, "err", err)
		} else {
			slog.Info("mcp tool", "tool", name, "team", teamID, "duration_ms", d, "status", status)
		}
		return result, err
	}
}
```
(Adjust to the actual handler-func types used at each site — some registrations pass plain funcs matching the signature; logTool's parameter type should be the common `func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error)` so both forms wrap.)

Wrap EVERY AddTool handler: `s.AddTool(tool, logTool("knowledge_store", HandleKnowledgeStore(...)))` etc. — enumerate via grep and cover all.

- [ ] **Step 4:** `go test ./internal/mcp/ 2>&1 | tail -2` + `go build ./...` — pass (existing handler tests call handlers directly, not via registration — unaffected).

---

### Task 3: Pipeline lifecycle + stage logs, stale message fix, cache debug logs

**Files:**
- Modify: `internal/pipeline/pipeline.go`, `internal/pipeline/cache.go`
- Test: extend pipeline tests

- [ ] **Step 1: Failing test** (buffer-backed handler around a minimal successful Run fixture):
```go
// TestRunLogsLifecycle: run one team → captured output contains
// "pipeline run started" with team + analysis_llm/agents_llm/improvement_llm fields, and
// "pipeline run finished" with status + duration_ms + clusters.
// TestStageFailureLogged: scoring LLM forced to error → captured output contains a WARN
// with stage="score" and team.
```

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3: Implement.**
- runForTeam after resolving clients/fingerprints (resolve improvement fingerprint too):
```go
	slog.Info("pipeline run started", "team", teamID, "trigger", trigger, "entries", len(entries),
		"analysis_llm", analysisFingerprint, "agents_llm", agentsFingerprint, "improvement_llm", improvementFingerprint)
```
(Place AFTER entries are listed; reorder locals as needed.)
- Before each FinishPipelineRun (or after, with the status variable):
```go
	slog.Info("pipeline run finished", "team", teamID, "status", status, "duration_ms", time.Since(runStart).Milliseconds(), "clusters", clustersFound, "errors", len(runErrs))
```
(Add `runStart := time.Now()` at top; cover the failed early-return paths too — factor a small closure if cleaner.)
- Every `runErrs = append(...)` site and logged-and-continue stage error: ensure a `slog.Warn` with `"stage"` (cluster|summarize|score|gaps|agent_gen|weak_signal|autotag), `"team"`, `"err"` fires (add where missing; add "team"+"stage" to existing ones).
- pipeline.go:232 message → `slog.Info("pipeline skipped: no LLM configured", "team", teamID, "trigger", trigger)`.
- cache.go: in each cached helper, after the lookup: `slog.Debug("analysis cache", "kind", cacheKindX, "hit", ok, "team", teamID)`.

- [ ] **Step 4:** `go test ./internal/pipeline/ -v 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"` then `go build ./... && go test ./... 2>&1 | tail -3` — ALL pass.

---

### Task 4: Final verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all pass; `cd web && npm run build` — clean (regression only).
- [ ] **Step 2:** Live-ish smoke WITHOUT touching real data: run the server binary briefly against a THROWAWAY DB copy with LOG_LEVEL=info on a spare port and confirm stderr shows the new log shapes — e.g.:
```bash
cd /Users/dsandor/Projects/memory && cp knowledge.db /tmp/obs-smoke.db && \
DATABASE_PATH=/tmp/obs-smoke.db HTTP_ADDR=:18099 DEV_BYPASS_AUTH=true LOG_LEVEL=info \
  timeout 20 go run ./cmd/server > /tmp/obs-smoke.log 2>&1 & sleep 12 && \
curl -s -X POST http://localhost:18099/api/pipeline/trigger >/dev/null; sleep 6; \
grep -E "pipeline run (started|finished)|llm |mcp tool" /tmp/obs-smoke.log | head -10; \
rm -f /tmp/obs-smoke.db* /tmp/obs-smoke.log
```
Expected: at least `pipeline run started`/`finished` lines (with llm fingerprints) appear; adapt port/waits as needed (timeout kills the server). If `go run` startup needs more time, lengthen sleeps. Report what appeared.
- [ ] **Step 3:** Report. **Do not commit.**
