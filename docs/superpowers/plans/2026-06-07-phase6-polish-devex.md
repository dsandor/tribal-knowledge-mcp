# Phase 6 — Polish & Developer Experience

**Date:** 2026-06-07
**Status:** pending
**Module:** `github.com/dsandor/memory`
**Go version:** 1.25
**CGO:** required (sqlite-vec)

---

## Overview

Phase 6 hardens the server for production deployment and reduces friction for new contributors and operators. It does not introduce new domain features — it makes everything built in Phases 1-5 observable, containerizable, documentable, and reproducible from a clean checkout.

Scope exclusions (deferred):
- PostgreSQL/pgvector storage adapter
- Onboarding wizard UI page

Exit criteria: `docker compose up` starts the full stack; README onboards a new developer in under 10 minutes; binary passes its own `/health` endpoint; all Go tests pass; Vite production build is clean.

---

## Task 1 — Structured Logging with `slog`

**Goal:** Replace every `log.Printf` / `log.Fatalf` / `log.Printf` call across the codebase with `log/slog`, configurable at startup via `LOG_LEVEL` env var.

### 1.1 Add `LogLevel` field to Config

**File:** `/Users/dsandor/Projects/memory/internal/config/config.go`

Add the field to the `Config` struct:

```go
LogLevel string // debug | info | warn | error  (default: info)
```

Add parsing in `Load()` after the existing fields:

```go
logLevel := envOrDefault("LOG_LEVEL", "info")
switch logLevel {
case "debug", "info", "warn", "error":
    // valid
default:
    return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: must be debug, info, warn, or error", logLevel)
}
```

Return value in the struct literal:

```go
LogLevel: logLevel,
```

Checkbox steps:
- [ ] Add `LogLevel string` to `Config` struct
- [ ] Add validation block in `Load()`
- [ ] Return `LogLevel` in the struct literal

### 1.2 Initialize `slog` in `main.go`

**File:** `/Users/dsandor/Projects/memory/cmd/server/main.go`

Remove the `"log"` import. Add `"log/slog"` and `"os"` (already present).

Immediately after `cfg, err := config.Load()` succeeds, initialize the global logger:

```go
var slogLevel slog.Level
switch cfg.LogLevel {
case "debug":
    slogLevel = slog.LevelDebug
case "warn":
    slogLevel = slog.LevelWarn
case "error":
    slogLevel = slog.LevelError
default:
    slogLevel = slog.LevelInfo
}
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slogLevel,
})))
```

Replace every remaining `log.Printf` / `log.Fatalf` call in `main.go`:

| Old call | Replacement |
|---|---|
| `log.Fatalf("load config: %v", err)` | `slog.Error("load config", "err", err); os.Exit(1)` |
| `log.Fatalf("open storage: %v", err)` | `slog.Error("open storage", "err", err); os.Exit(1)` |
| `log.Fatalf("sub fs: %v", err)` | `slog.Error("sub embedded fs", "err", err); os.Exit(1)` |
| `log.Printf("HTTP server listening on %s", cfg.HTTPAddr)` | `slog.Info("HTTP server listening", "addr", cfg.HTTPAddr)` |
| `log.Printf("HTTP server error: %v", err)` | `slog.Error("HTTP server error", "err", err)` |
| `log.Printf("serve: %v", err)` | `slog.Error("MCP stdio serve", "err", err)` |
| `log.Printf("HTTP shutdown: %v", err)` | `slog.Warn("HTTP shutdown error", "err", err)` |
| `log.Printf("WARNING: failed to bootstrap superadmin key: %v", err)` | `slog.Warn("superadmin bootstrap failed", "err", err)` |
| `log.Printf("Superadmin API key bootstrapped")` | `slog.Info("superadmin API key bootstrapped")` |

Checkbox steps:
- [ ] Replace `"log"` import with `"log/slog"`
- [ ] Add logger initialization block after `config.Load()`
- [ ] Replace all `log.Printf` / `log.Fatalf` calls per table above

### 1.3 Replace `log` calls in internal packages

Search all internal packages for `log.Printf` / `log.Fatalf` / `log.Println` and replace with `slog` equivalents. Key files to audit:

**Files likely containing `log.*` calls:**
- `/Users/dsandor/Projects/memory/internal/pipeline/pipeline.go` — use `slog.Info`/`slog.Warn`/`slog.Error` with structured fields (e.g., `"phase"`, `"cluster_id"`)
- `/Users/dsandor/Projects/memory/internal/mcp/server.go` — replace startup/registration logs
- `/Users/dsandor/Projects/memory/internal/storage/sqlite.go` — replace any migration/init logs
- `/Users/dsandor/Projects/memory/internal/web/server.go` — request error logs

Pattern for search: `grep -r 'log\.' internal/ cmd/` — every match that is not `slog.` must be converted.

Convention for structured fields:
- Use lowercase snake_case keys: `"err"`, `"team_id"`, `"entry_id"`, `"phase"`, `"duration_ms"`
- All errors passed as `"err", err`
- Never use `fmt.Sprintf` inside an `slog` call — pass values as separate key/value pairs

Checkbox steps:
- [ ] Run `grep -rn '"log"' internal/ cmd/` and list all files importing `"log"`
- [ ] Convert each file: replace import, replace calls
- [ ] Verify no `"log"` import remains in non-test files

### 1.4 Update `config_test.go`

**File:** `/Users/dsandor/Projects/memory/internal/config/config_test.go`

Add test cases for `LOG_LEVEL`:
- Valid values: `"debug"`, `"info"`, `"warn"`, `"error"` — all load without error
- Invalid value: `"verbose"` returns an error containing `"LOG_LEVEL"`
- Default (env unset): `LogLevel` field equals `"info"`

Checkbox steps:
- [ ] Add `TestLoad_LogLevel` function covering the four cases above

---

## Task 2 — Enhanced `/health` Endpoint

**Goal:** Replace the stub 200-OK `/health` handler with a JSON response that reports the liveness and readiness of each subsystem.

### 2.1 Define the response schema

```go
// HealthResponse is the JSON body returned by GET /health.
type HealthResponse struct {
    Status    string                     `json:"status"`    // "ok" | "degraded" | "error"
    Timestamp string                     `json:"timestamp"` // RFC3339
    Version   string                     `json:"version"`   // build-time injected, default "dev"
    Components map[string]ComponentStatus `json:"components"`
}

type ComponentStatus struct {
    Status  string `json:"status"`           // "ok" | "error" | "unknown"
    Message string `json:"message,omitempty"`
    LatencyMs int64 `json:"latency_ms,omitempty"`
}
```

### 2.2 Add `Ping` method to `StorageStore`

**File:** `/Users/dsandor/Projects/memory/internal/storage/storage.go`

Add to the relevant storage interface (or as a concrete method on `SQLiteStore`):

```go
// Ping verifies the storage connection is alive. Returns nil on success.
Ping(ctx context.Context) error
```

**File:** `/Users/dsandor/Projects/memory/internal/storage/sqlite.go`

Implement `Ping` by executing a lightweight query:

```go
func (s *SQLiteStore) Ping(ctx context.Context) error {
    _, err := s.db.ExecContext(ctx, "SELECT 1")
    return err
}
```

### 2.3 Add `Ping` method to `OllamaEmbedder`

**File:** `/Users/dsandor/Projects/memory/internal/embedding/ollama.go`

```go
// Ping checks whether the Ollama endpoint is reachable by calling GET /api/tags.
// Returns nil if the HTTP response status is 2xx within 3 seconds.
func (o *OllamaEmbedder) Ping(ctx context.Context) error {
    reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()
    req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, o.baseURL+"/api/tags", nil)
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    resp.Body.Close()
    if resp.StatusCode >= 300 {
        return fmt.Errorf("ollama ping: status %d", resp.StatusCode)
    }
    return nil
}
```

### 2.4 Expose pipeline last-run status

**File:** `/Users/dsandor/Projects/memory/internal/pipeline/pipeline.go`

Add a thread-safe accessor to the `Pipeline` struct:

```go
type LastRunStatus struct {
    StartedAt  time.Time
    FinishedAt time.Time
    Error      error
    Running    bool
}

func (p *Pipeline) LastRun() LastRunStatus { ... } // returns copy under mutex
```

The `Pipeline` pointer must be passed to `web.Server` so the health handler can read it.

### 2.5 Wire health handler

**File:** `/Users/dsandor/Projects/memory/internal/web/server.go`

Add `pipeline` field to `Server` struct (interface or concrete, either is fine since this is internal):

```go
type PipelineStatus interface {
    LastRun() pipeline.LastRunStatus
}
```

Add `WithPipeline(p PipelineStatus) *Server` builder method.

Replace the stub `/health` route with:

```go
r.Get("/health", s.handleHealth)
```

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    start := time.Now()

    health := HealthResponse{
        Timestamp:  start.UTC().Format(time.RFC3339),
        Version:    buildVersion, // package-level var set at build time
        Components: make(map[string]ComponentStatus),
    }

    // Storage ping
    t0 := time.Now()
    if err := s.store.Ping(ctx); err != nil {
        health.Components["storage"] = ComponentStatus{Status: "error", Message: err.Error(), LatencyMs: time.Since(t0).Milliseconds()}
    } else {
        health.Components["storage"] = ComponentStatus{Status: "ok", LatencyMs: time.Since(t0).Milliseconds()}
    }

    // Embedding ping
    t0 = time.Now()
    if s.embedder != nil {
        if err := s.embedder.Ping(ctx); err != nil {
            health.Components["embedding"] = ComponentStatus{Status: "error", Message: err.Error(), LatencyMs: time.Since(t0).Milliseconds()}
        } else {
            health.Components["embedding"] = ComponentStatus{Status: "ok", LatencyMs: time.Since(t0).Milliseconds()}
        }
    } else {
        health.Components["embedding"] = ComponentStatus{Status: "unknown", Message: "no embedder configured"}
    }

    // Pipeline last-run
    if s.pipeline != nil {
        lr := s.pipeline.LastRun()
        msg := ""
        if lr.Error != nil {
            msg = lr.Error.Error()
        }
        st := "ok"
        if lr.Error != nil {
            st = "error"
        }
        if lr.Running {
            st = "running"
        }
        health.Components["pipeline"] = ComponentStatus{Status: st, Message: msg}
    } else {
        health.Components["pipeline"] = ComponentStatus{Status: "unknown", Message: "no anthropic key configured"}
    }

    // Aggregate status
    health.Status = "ok"
    for _, c := range health.Components {
        if c.Status == "error" {
            health.Status = "degraded"
            break
        }
    }

    statusCode := http.StatusOK
    if health.Status != "ok" {
        statusCode = http.StatusServiceUnavailable
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    _ = json.NewEncoder(w).Encode(health)
}
```

**Build-time version injection:**

In `Makefile`, pass `-ldflags`:

```makefile
LDFLAGS := -ldflags "-X github.com/dsandor/memory/internal/web.buildVersion=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)"
```

Checkbox steps:
- [ ] Add `Ping(ctx context.Context) error` to storage interface and SQLiteStore
- [ ] Add `Ping(ctx context.Context) error` to OllamaEmbedder
- [ ] Add `LastRun()` accessor to Pipeline with mutex safety
- [ ] Add `PipelineStatus` interface and `WithPipeline` builder to web.Server
- [ ] Add `embedder` field to web.Server; expose via `WithEmbedder` builder
- [ ] Implement `handleHealth` in web.Server
- [ ] Update route registration to call `handleHealth`
- [ ] Add `LDFLAGS` to Makefile `build` target
- [ ] Write `TestHandleHealth` with table-driven cases (storage ok/error, embedder ok/error)

---

## Task 3 — Graceful Shutdown

**Goal:** Ensure that when SIGINT/SIGTERM is received, in-flight HTTP requests complete, the pipeline finishes its current stage (not a full run), and stdio MCP exits cleanly before the process terminates. The current code has a structural issue: `server.ServeStdio` blocks until stdin closes, so the HTTP shutdown after it is a dead path in practice.

### 3.1 Restructure shutdown sequence in `main.go`

The correct ordering for this binary (which serves both stdio MCP and HTTP concurrently):

1. Signal received → `cancel()` fires (already wired via `signal.NotifyContext`)
2. Pipeline's `Start(ctx)` observes `ctx.Done()` → finishes current stage, returns
3. HTTP server receives `Shutdown` signal → drains in-flight requests
4. stdio MCP goroutine unblocks (stdin closes or context cancels)
5. Process exits

Current issue: `server.ServeStdio(mcpServer)` is called on the main goroutine and blocks. Move it to a goroutine. Add a `sync.WaitGroup` to track the HTTP server goroutine.

New shutdown block in `main.go`:

```go
// Run MCP stdio in a goroutine so signal handling is not blocked.
mcpDone := make(chan struct{})
go func() {
    defer close(mcpDone)
    if err := server.ServeStdio(mcpServer); err != nil {
        slog.Error("MCP stdio serve error", "err", err)
    }
}()

// Block until signal or MCP exit.
select {
case <-ctx.Done():
    slog.Info("shutdown signal received")
case <-mcpDone:
    slog.Info("MCP stdio transport closed")
}

// Shutdown HTTP server with a 15-second drain window.
shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
defer shutCancel()
slog.Info("draining HTTP server", "timeout_s", 15)
if err := httpServer.Shutdown(shutCtx); err != nil {
    slog.Warn("HTTP shutdown incomplete", "err", err)
} else {
    slog.Info("HTTP server stopped cleanly")
}

// Wait for MCP goroutine if it hasn't finished yet.
<-mcpDone
slog.Info("server exit")
```

### 3.2 Pipeline stage-aware drain

**File:** `/Users/dsandor/Projects/memory/internal/pipeline/pipeline.go`

The `Pipeline.Start(ctx)` goroutine must finish the stage it is currently executing before returning, not abort mid-stage. Implement with a second internal context:

```go
// stageMu protects stageCancel and running.
// When the outer ctx is cancelled, we let the current stage finish by
// NOT propagating ctx cancellation into per-stage calls, but we do stop
// scheduling new runs.

func (p *Pipeline) Start(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                // Let in-progress stage finish by waiting on stageDone.
                p.mu.Lock()
                running := p.running
                p.mu.Unlock()
                if running {
                    <-p.stageDone // channel closed by runOnce when stage finishes
                }
                slog.Info("pipeline stopped")
                return
            case <-p.ticker.C:
                p.maybeRun()
            case <-p.triggerCh:
                p.maybeRun()
            }
        }
    }()
}
```

Add `stageDone chan struct{}` field to `Pipeline`. `runOnce` creates a new `stageDone`, runs the stage, then closes it.

Checkbox steps:
- [ ] Move `server.ServeStdio` call to a goroutine in `main.go`
- [ ] Replace the `select` + shutdown block with the pattern shown above
- [ ] Increase HTTP drain timeout from 5 s to 15 s
- [ ] Add `stageDone chan struct{}` to `Pipeline` struct
- [ ] Update `runOnce`/`maybeRun` to signal stage completion
- [ ] Update `Start` to wait on `stageDone` before returning on ctx cancel
- [ ] Verify with `go test -race ./internal/pipeline/...`

---

## Task 4 — Dockerfile (Multi-Stage CGO Build)

**Goal:** A single `Dockerfile` that produces a minimal production image. CGO must be enabled for sqlite-vec. The final base image is `debian:bookworm-slim` (not scratch) because sqlite-vec requires glibc at runtime.

**File:** `/Users/dsandor/Projects/memory/Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1

# ─── Stage 1: Build React SPA ───────────────────────────────────────────────
FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --prefer-offline
COPY web/ ./
RUN npm run build
# Output: /app/web/dist/

# ─── Stage 2: Build Go binary ───────────────────────────────────────────────
FROM golang:1.25-bookworm AS go-builder

# Install gcc and sqlite build dependencies required by CGO / sqlite-vec.
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Download dependencies before copying source (layer cache).
COPY go.mod go.sum ./
RUN go mod download

# Copy web build output so go:embed finds web/dist/.
COPY --from=web-builder /app/web/dist ./web/dist

# Copy the rest of the source.
COPY . .

# Inject git version; fall back to "docker" if .git is absent.
ARG VERSION=docker
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-X github.com/dsandor/memory/internal/web.buildVersion=${VERSION} -extldflags '-static'" \
    -tags netgo \
    -o /app/server \
    ./cmd/server

# ─── Stage 3: Runtime image ─────────────────────────────────────────────────
FROM debian:bookworm-slim

# ca-certificates for outbound TLS (Anthropic API, Ollama).
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user.
RUN useradd -r -u 1001 -g root memory
USER 1001

WORKDIR /data

COPY --from=go-builder /app/server /usr/local/bin/server

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]
```

Notes:
- `-tags netgo` + `-extldflags '-static'` produce a statically linked binary that works on bookworm-slim without additional shared libraries.
- The `web/dist/` is copied into the Go builder stage so `//go:embed dist` resolves correctly at build time.
- `CGO_ENABLED=1` is explicitly set; the default in the `golang:1.25-bookworm` image is 0.
- `VERSION` build arg allows CI to inject `git describe --tags` output.

Checkbox steps:
- [ ] Create `/Users/dsandor/Projects/memory/Dockerfile`
- [ ] Verify `docker build -t memory:local .` completes without error
- [ ] Verify `docker run --rm -e DEV_BYPASS_AUTH=true memory:local` starts and `/health` returns JSON
- [ ] Confirm image size is under 150 MB

---

## Task 5 — docker-compose.yml and .env.example

**Goal:** One command (`docker compose up`) starts the server and an optional Ollama sidecar. `.env.example` documents every required and optional env var.

### 5.1 docker-compose.yml

**File:** `/Users/dsandor/Projects/memory/docker-compose.yml`

```yaml
version: "3.9"

services:
  server:
    build:
      context: .
      dockerfile: Dockerfile
      args:
        VERSION: "${VERSION:-dev}"
    image: memory:${VERSION:-dev}
    ports:
      - "${HTTP_PORT:-8080}:8080"
    volumes:
      # Persist SQLite database on the host.
      - "${DATA_DIR:-./data}:/data"
    environment:
      DATABASE_PATH: /data/knowledge.db
      OLLAMA_URL: http://ollama:11434
      OLLAMA_MODEL: "${OLLAMA_MODEL:-nomic-embed-text}"
      EMBEDDING_DIM: "${EMBEDDING_DIM:-768}"
      HTTP_ADDR: ":8080"
      LOG_LEVEL: "${LOG_LEVEL:-info}"
      TEAM_ID: "${TEAM_ID:-default}"
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY:-}"
      ANTHROPIC_MODEL: "${ANTHROPIC_MODEL:-claude-haiku-4-5-20251001}"
      AGENT_MODEL: "${AGENT_MODEL:-claude-sonnet-4-6}"
      PIPELINE_INTERVAL: "${PIPELINE_INTERVAL:-1h}"
      PIPELINE_MIN_ENTRIES: "${PIPELINE_MIN_ENTRIES:-10}"
      CLUSTER_THRESHOLD: "${CLUSTER_THRESHOLD:-0.85}"
      SUPERADMIN_KEY: "${SUPERADMIN_KEY:-}"
      DEV_BYPASS_AUTH: "${DEV_BYPASS_AUTH:-false}"
    depends_on:
      ollama:
        condition: service_started
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    restart: unless-stopped

  ollama:
    image: ollama/ollama:latest
    ports:
      - "${OLLAMA_PORT:-11434}:11434"
    volumes:
      - "${OLLAMA_DATA_DIR:-./ollama-data}:/root/.ollama"
    # GPU passthrough (uncomment if host has NVIDIA drivers):
    # deploy:
    #   resources:
    #     reservations:
    #       devices:
    #         - driver: nvidia
    #           count: all
    #           capabilities: [gpu]
    restart: unless-stopped
    profiles:
      - ollama   # only started when: docker compose --profile ollama up
```

Using `profiles: [ollama]` on the Ollama service means `docker compose up` starts only the server (pointing at a local Ollama or external), while `docker compose --profile ollama up` starts both.

### 5.2 .env.example

**File:** `/Users/dsandor/Projects/memory/`.env.example`

```dotenv
# ── Server ──────────────────────────────────────────────────────────────────
HTTP_PORT=8080
HTTP_ADDR=:8080
LOG_LEVEL=info          # debug | info | warn | error
DATA_DIR=./data         # host path for SQLite persistence
VERSION=dev

# ── Team ────────────────────────────────────────────────────────────────────
TEAM_ID=default
SUPERADMIN_KEY=          # set to a long random string in production

# ── Auth ────────────────────────────────────────────────────────────────────
DEV_BYPASS_AUTH=false    # set true ONLY in development; never in production

# ── Embeddings (Ollama) ──────────────────────────────────────────────────────
OLLAMA_URL=http://localhost:11434
OLLAMA_MODEL=nomic-embed-text
OLLAMA_PORT=11434        # exposed host port when using docker compose --profile ollama
OLLAMA_DATA_DIR=./ollama-data
EMBEDDING_DIM=768

# ── LLM (Anthropic) ──────────────────────────────────────────────────────────
ANTHROPIC_API_KEY=       # required for pipeline, agent generation, prompt_suggest
ANTHROPIC_MODEL=claude-haiku-4-5-20251001
AGENT_MODEL=claude-sonnet-4-6

# ── Pipeline ─────────────────────────────────────────────────────────────────
PIPELINE_INTERVAL=1h           # Go duration: 30m, 2h, etc.
PIPELINE_MIN_ENTRIES=10
CLUSTER_THRESHOLD=0.85         # cosine similarity threshold (0,1]

# ── MCP HTTP/SSE transport (optional) ────────────────────────────────────────
MCP_HTTP_ADDR=           # e.g. :9090  — leave empty to disable HTTP MCP
MCP_HTTP_PATH=/mcp
```

Checkbox steps:
- [ ] Create `/Users/dsandor/Projects/memory/docker-compose.yml`
- [ ] Create `/Users/dsandor/Projects/memory/.env.example`
- [ ] Verify `docker compose config` reports no errors
- [ ] Verify `docker compose --profile ollama up --dry-run` shows both services

---

## Task 6 — Seed Data Script

**Goal:** A Python script at `scripts/seed.py` that POSTs realistic knowledge entries to the REST API across three domains. Uses `DEV_BYPASS_AUTH=true` or an explicit API key via `--api-key`. Idempotent: skips entries that already exist (matches on title hash).

**File:** `/Users/dsandor/Projects/memory/scripts/seed.py`

```python
#!/usr/bin/env python3
"""
Seed the tribal knowledge MCP server with realistic example entries.

Usage:
    python scripts/seed.py                        # defaults to http://localhost:8080
    python scripts/seed.py --base-url http://...  # custom server
    python scripts/seed.py --api-key sk-...       # provide auth header
    python scripts/seed.py --dry-run              # print payloads without posting

Requires: Python 3.9+ and the 'requests' package (pip install requests).
"""

import argparse
import json
import sys
import time
from dataclasses import dataclass, asdict
from typing import Optional

try:
    import requests
except ImportError:
    print("ERROR: 'requests' package not found. Run: pip install requests", file=sys.stderr)
    sys.exit(1)

# ── Seed data ─────────────────────────────────────────────────────────────────

SEED_ENTRIES = [
    # Domain: financial-analysis
    {
        "title": "Earnings call transcript prompt template",
        "content": (
            "When analysing an earnings call transcript, structure your prompt as follows:\n"
            "1. CONTEXT: Provide the company ticker, fiscal quarter, and analyst consensus EPS.\n"
            "2. TASK: Ask the LLM to extract (a) management tone, (b) forward guidance deltas vs prior quarter, "
            "(c) top three risk factors mentioned.\n"
            "3. OUTPUT FORMAT: Request a markdown table with columns: Topic | Signal | Sentiment | Confidence.\n"
            "Teams using this template report 40% reduction in missed signals compared to free-form prompts."
        ),
        "type": "prompt_template",
        "domain": "financial-analysis",
        "tags": ["earnings", "transcript", "prompt-engineering"],
    },
    {
        "title": "Sector rotation signal checklist",
        "content": (
            "Before concluding a sector rotation recommendation, verify:\n"
            "- Relative strength index (RSI) cross-sector comparison over 90-day window\n"
            "- Fed funds rate direction vs historical sector beta\n"
            "- Yield curve slope (2-10 spread) compared to prior rotation cycles\n"
            "- Commodity index correlation for energy/materials sectors\n"
            "Prompt the LLM with: 'Given [data], identify which sectors are early/mid/late cycle and assign confidence 1-5.'"
        ),
        "type": "checklist",
        "domain": "financial-analysis",
        "tags": ["sector-rotation", "macro", "checklist"],
    },
    {
        "title": "Avoid recency bias in LLM stock reports",
        "content": (
            "LLMs weight recent training data heavily. When generating a stock report:\n"
            "- Explicitly provide 5-year CAGR alongside TTM growth to anchor long-term context.\n"
            "- Ask the model to compare current P/E to the 10-year median, not just the 52-week range.\n"
            "- Anti-pattern: asking 'Is this stock a good buy?' without date context — model anchors on last known price.\n"
            "- Best practice: include 'As of [date], with the stock at [price]…' in every prompt."
        ),
        "type": "best_practice",
        "domain": "financial-analysis",
        "tags": ["bias", "stock-analysis", "prompt-engineering"],
    },

    # Domain: software-engineering
    {
        "title": "Code review prompt for security vulnerabilities",
        "content": (
            "Use this prompt structure when asking an LLM to review code for security issues:\n\n"
            "```\n"
            "Review the following [language] code for security vulnerabilities.\n"
            "Focus on: SQL injection, XSS, insecure deserialization, hardcoded secrets, "
            "improper error handling that leaks stack traces.\n"
            "For each finding: describe the vulnerability, assign CVSS severity (Low/Med/High/Critical), "
            "and provide a remediation code snippet.\n"
            "```\n\n"
            "Attach the code block after the prompt. Teams report this yields 3x more actionable findings "
            "than 'look for bugs in this code'."
        ),
        "type": "prompt_template",
        "domain": "software-engineering",
        "tags": ["security", "code-review", "prompt-engineering"],
    },
    {
        "title": "Architecture decision record (ADR) generation prompt",
        "content": (
            "To generate a high-quality ADR using an LLM:\n"
            "1. State the decision context: system name, scale, constraints, and the options considered.\n"
            "2. Ask the model to produce: Title, Status, Context, Decision, Consequences (positive and negative), "
            "and Alternatives Considered sections.\n"
            "3. Include a 'Risks' section by appending: 'List the top three risks and a mitigation for each.'\n"
            "ADRs generated this way consistently pass architecture review without major revisions."
        ),
        "type": "prompt_template",
        "domain": "software-engineering",
        "tags": ["architecture", "adr", "documentation"],
    },
    {
        "title": "Debugging prompts: give the LLM full context",
        "content": (
            "When asking an LLM to debug a failing test or runtime error, always include:\n"
            "- The full error message and stack trace (not a summary)\n"
            "- The relevant code section (not the entire file)\n"
            "- What you have already tried\n"
            "- The Go/Python/Rust version and OS\n\n"
            "Anti-pattern: 'This function doesn't work, fix it.' — LLM will produce a generic response.\n"
            "Best practice: 'This function fails with [error] on line [N] when input is [X]. "
            "I tried [Y] and [Z]. Here is the code: [snippet].'"
        ),
        "type": "best_practice",
        "domain": "software-engineering",
        "tags": ["debugging", "context", "prompt-engineering"],
    },

    # Domain: data-science
    {
        "title": "EDA summary prompt for tabular datasets",
        "content": (
            "Paste the output of `df.describe()` and `df.info()` into this prompt:\n\n"
            "```\n"
            "You are a senior data scientist. Given the following dataset summary:\n"
            "[INSERT df.describe() output]\n"
            "[INSERT df.info() output]\n\n"
            "Identify: (1) columns with high missing-value rates (>20%), "
            "(2) potential outlier columns based on mean/std ratio, "
            "(3) columns that are likely categorical despite numeric dtype, "
            "(4) recommended feature engineering steps.\n"
            "Output a markdown report with a findings table and recommendations list.\n"
            "```"
        ),
        "type": "prompt_template",
        "domain": "data-science",
        "tags": ["eda", "pandas", "feature-engineering"],
    },
    {
        "title": "Model evaluation prompt: avoid cherry-picked metrics",
        "content": (
            "When asking an LLM to evaluate a model's performance, require a complete picture:\n"
            "- For classification: accuracy, precision, recall, F1, AUC-ROC, and confusion matrix interpretation.\n"
            "- For regression: MAE, RMSE, R², and residual plot description.\n"
            "- Always ask: 'Is the dataset class-balanced? If not, which metric should be prioritized and why?'\n\n"
            "Anti-pattern: reporting only accuracy on an imbalanced dataset — LLM will validate a useless model.\n"
            "Pair this with: 'What would a naive baseline (majority class / mean prediction) score on these same metrics?'"
        ),
        "type": "best_practice",
        "domain": "data-science",
        "tags": ["model-evaluation", "metrics", "imbalance"],
    },
    {
        "title": "Reproducibility checklist for ML experiments",
        "content": (
            "Before sharing an ML experiment result, verify:\n"
            "- Random seeds set for numpy, torch/tensorflow, and the train/test split\n"
            "- Library versions pinned in requirements.txt or environment.yaml\n"
            "- Dataset version and preprocessing steps documented\n"
            "- Hyperparameter search log saved (not just the best params)\n\n"
            "Prompt template for LLM-assisted review:\n"
            "'Review this ML experiment setup for reproducibility issues. "
            "Here is my code: [snippet] and my results: [metrics]. "
            "Flag anything that would prevent a colleague from reproducing these results exactly.'"
        ),
        "type": "checklist",
        "domain": "data-science",
        "tags": ["reproducibility", "mlops", "best-practices"],
    },
]


# ── Client ────────────────────────────────────────────────────────────────────

def post_entry(base_url: str, headers: dict, entry: dict, dry_run: bool) -> Optional[str]:
    url = f"{base_url.rstrip('/')}/api/knowledge"
    if dry_run:
        print(f"[DRY RUN] POST {url}")
        print(json.dumps(entry, indent=2))
        return None
    resp = requests.post(url, json=entry, headers=headers, timeout=15)
    if resp.status_code == 409:
        print(f"  SKIP (already exists): {entry['title']}")
        return None
    resp.raise_for_status()
    data = resp.json()
    return data.get("id")


def pull_team_id(base_url: str, headers: dict) -> Optional[str]:
    url = f"{base_url.rstrip('/')}/api/settings"
    try:
        resp = requests.get(url, headers=headers, timeout=5)
        if resp.ok:
            return resp.json().get("team_id")
    except Exception:
        pass
    return None


def main():
    parser = argparse.ArgumentParser(description="Seed the tribal knowledge MCP server")
    parser.add_argument("--base-url", default="http://localhost:8080", help="Server base URL")
    parser.add_argument("--api-key", default="", help="API key for Authorization header")
    parser.add_argument("--dry-run", action="store_true", help="Print payloads without POSTing")
    parser.add_argument("--delay", type=float, default=0.2, help="Seconds between requests (default 0.2)")
    args = parser.parse_args()

    headers = {"Content-Type": "application/json"}
    if args.api_key:
        headers["Authorization"] = f"Bearer {args.api_key}"

    print(f"Seeding {len(SEED_ENTRIES)} entries to {args.base_url}")
    if args.dry_run:
        print("(dry-run mode — no requests will be sent)\n")

    created = 0
    skipped = 0
    errors = 0

    for entry in SEED_ENTRIES:
        print(f"  POST [{entry['domain']}] {entry['title'][:60]}...")
        try:
            eid = post_entry(args.base_url, headers, entry, args.dry_run)
            if eid:
                print(f"    -> created: {eid}")
                created += 1
            elif not args.dry_run:
                skipped += 1
        except requests.HTTPError as e:
            print(f"    ERROR: {e.response.status_code} {e.response.text[:120]}", file=sys.stderr)
            errors += 1
        except Exception as e:
            print(f"    ERROR: {e}", file=sys.stderr)
            errors += 1
        if not args.dry_run:
            time.sleep(args.delay)

    print(f"\nDone. created={created} skipped={skipped} errors={errors}")
    if errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
```

Checkbox steps:
- [ ] Create `/Users/dsandor/Projects/memory/scripts/seed.py`
- [ ] Make executable: `chmod +x scripts/seed.py`
- [ ] Verify with `python scripts/seed.py --dry-run` — all 9 entries print without error
- [ ] Verify with server running and `DEV_BYPASS_AUTH=true`: `python scripts/seed.py` creates all entries and `/api/knowledge` returns 9 items
- [ ] Run a second time and verify all entries are skipped (idempotency)

---

## Task 7 — README.md

**Goal:** A root-level README that onboards a new developer or operator in under 10 minutes. No marketing fluff — just the minimum viable documentation to go from clone to working MCP connection.

**File:** `/Users/dsandor/Projects/memory/README.md`

Sections to include (write in full, prose below describes required content):

### Sections

**1. What This Is**
One paragraph. Tribal knowledge MCP server: captures team knowledge, continuously clusters and analyzes it, generates specialized AI agents per domain, and serves everything via MCP protocol and an embedded web UI. Mention the financial-analyst motivation but make clear it is general-purpose.

**2. Quick Start (Docker)**
```bash
git clone https://github.com/dsandor/memory
cd memory
cp .env.example .env
# Edit .env: set ANTHROPIC_API_KEY and SUPERADMIN_KEY
docker compose --profile ollama up -d
open http://localhost:8080
```
Note: first run pulls the Ollama image and the `nomic-embed-text` model; may take a few minutes on first start.

**3. Quick Start (Local Build)**
Prerequisites: Go 1.25+, Node.js 22+, gcc (for CGO/sqlite-vec), Ollama running locally.

```bash
make web          # npm ci + vite build
make build        # go build with CGO_ENABLED=1
./server
```

**4. Seeding Example Data**
```bash
DEV_BYPASS_AUTH=true ./server &
pip install requests
python scripts/seed.py
```

**5. Connecting via MCP**

Claude Desktop (`~/.config/claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/path/to/server",
      "env": {
        "DATABASE_PATH": "/path/to/knowledge.db",
        "ANTHROPIC_API_KEY": "sk-ant-...",
        "DEV_BYPASS_AUTH": "true"
      }
    }
  }
}
```

Cursor (`~/.cursor/mcp.json`):
```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/path/to/server",
      "env": { "DATABASE_PATH": "/path/to/knowledge.db" }
    }
  }
}
```

HTTP/SSE transport (for remote clients):
```bash
MCP_HTTP_ADDR=:9090 ./server
# Connect any SSE-capable MCP client to http://localhost:9090/mcp
```

**6. Environment Variable Reference**

Full table with columns: Variable | Default | Required | Description. Include every field in `Config` struct. Mark `ANTHROPIC_API_KEY` as "required for pipeline/agents". Mark `DEV_BYPASS_AUTH` as "never set in production".

| Variable | Default | Required | Description |
|---|---|---|---|
| `DATABASE_PATH` | `knowledge.db` | no | SQLite database file path |
| `OLLAMA_URL` | `http://localhost:11434` | no | Ollama server URL for embeddings |
| `OLLAMA_MODEL` | `nomic-embed-text` | no | Embedding model name |
| `EMBEDDING_DIM` | `768` | no | Embedding vector dimension; must match the model |
| `HTTP_ADDR` | `:8080` | no | Address the HTTP server binds to |
| `LOG_LEVEL` | `info` | no | Log verbosity: `debug`, `info`, `warn`, `error` |
| `TEAM_ID` | `default` | no | Default team identifier |
| `ANTHROPIC_API_KEY` | — | yes (pipeline/agents) | Anthropic API key |
| `ANTHROPIC_MODEL` | `claude-haiku-4-5-20251001` | no | Model for pipeline analysis |
| `AGENT_MODEL` | `claude-sonnet-4-6` | no | Model for agent generation |
| `PIPELINE_INTERVAL` | `1h` | no | How often the analysis pipeline runs |
| `PIPELINE_MIN_ENTRIES` | `10` | no | Minimum entries before pipeline triggers |
| `CLUSTER_THRESHOLD` | `0.85` | no | Cosine similarity threshold for clustering |
| `SUPERADMIN_KEY` | — | no | Raw API key bootstrapped as superadmin on first run |
| `DEV_BYPASS_AUTH` | `false` | no | Skip auth; inject superadmin context. NEVER use in production |
| `OIDC_CLIENT_SECRET` | — | no | OIDC provider client secret |
| `MCP_HTTP_ADDR` | — | no | Address for HTTP/SSE MCP transport. Leave empty to disable |
| `MCP_HTTP_PATH` | `/mcp` | no | HTTP path for SSE MCP endpoint |

**7. MCP Tools Reference**

Brief table: Tool name | Description. Include all tools from Phases 1-5.

**8. Development**

```bash
make test          # go test ./...
make web           # build React SPA
make build         # build Go binary
make clean         # remove build artifacts
```

**9. Architecture Overview**

One ASCII diagram of the major layers:

```
┌──────────────────────────────────────────────────────┐
│                   MCP Clients                         │
│   Claude Desktop  /  Cursor / Zed / Remote SSE       │
└───────────────┬──────────────────────────────────────┘
                │ stdio or HTTP/SSE
┌───────────────▼──────────────────────────────────────┐
│                 server binary                         │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  MCP Layer │  │ REST HTTP API│  │ Embedded SPA │  │
│  └────────────┘  └──────────────┘  └──────────────┘  │
│  ┌────────────────────────────────────────────────┐   │
│  │            Analysis Pipeline (goroutine)        │   │
│  │   cluster → score → summarize → generate agent │   │
│  └────────────────────────────────────────────────┘   │
│  ┌────────────────────────────────────────────────┐   │
│  │        Storage (SQLite + sqlite-vec)            │   │
│  └────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
         │ embeddings          │ LLM calls
┌────────▼──────┐    ┌─────────▼──────────┐
│  Ollama       │    │  Anthropic API      │
│  (local/sidecar)   │  claude-haiku/sonnet│
└───────────────┘    └────────────────────┘
```

Checkbox steps:
- [ ] Create `/Users/dsandor/Projects/memory/README.md` with all 9 sections
- [ ] Verify all code blocks are syntactically correct (JSON, bash, etc.)
- [ ] Verify MCP config snippets match the actual env var names in config.go

---

## Task 8 — CHANGELOG.md and State/Roadmap Updates

**Goal:** Initialize CHANGELOG.md summarizing Phases 1-5, and update planning files to reflect Phase 6 is now in-progress.

### 8.1 CHANGELOG.md

**File:** `/Users/dsandor/Projects/memory/CHANGELOG.md`

Follow [Keep a Changelog](https://keepachangelog.com) format. One section per phase. No version numbers (semantic versioning will be added post-Phase 6). Use dates of implementation.

```markdown
# Changelog

All notable changes to this project are documented in this file.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased] — Phase 6: Polish & Developer Experience

### Added
- Structured logging via `log/slog` with `LOG_LEVEL` env var
- Enhanced `/health` endpoint with JSON component status (storage, embedding, pipeline)
- Graceful shutdown: in-flight HTTP drain + pipeline stage completion
- Multi-stage `Dockerfile` with CGO/sqlite-vec support
- `docker-compose.yml` with optional Ollama sidecar profile
- `.env.example` documenting all environment variables
- `scripts/seed.py`: seeds 9 realistic knowledge entries across 3 domains
- `README.md`: installation, MCP config snippets, env var reference, architecture diagram

---

## Phase 5 — REST API, Analytics & Team Model (2026-06-07)

### Added
- chi router with 14+ REST endpoints (`/api/knowledge`, `/api/clusters`, `/api/agents`, `/api/pipeline`, `/api/analytics`, `/api/settings`)
- Multi-tenant team scoping on all knowledge, agents, and API keys
- Role-based access control: `superadmin`, `admin`, `curator`, `member`
- API key authentication (team-scoped and per-user)
- Analytics endpoints: usage heatmaps, domain coverage gaps, contribution leaderboard
- Curator approval queue for agent draft → published workflow
- Settings GET/PUT for team configuration
- HTTP/SSE MCP transport for remote client connections
- MCP tools: `knowledge_search`, `knowledge_rate`, `prompt_suggest`
- MCP resources: `knowledge://team/top`, `knowledge://team/recent`, `knowledge://domain/{name}`, `knowledge://cluster/{id}`
- React analytics and settings pages wired to live REST data

---

## Phase 4 — Embedded Web UI (2026-06-06)

### Added
- React + TypeScript + Vite SPA compiled and embedded in Go binary via `//go:embed`
- shadcn/ui + Tailwind dark theme
- Pages: Dashboard, Knowledge Browser, Knowledge Detail, Clusters, Datasets, Agents, Agent Detail
- Agent Detail: full definition, source refs, version diff viewer, approve/reject draft
- Download flows: single agent export in Claude subagent MD, plain TXT, and JSON formats
- Go HTTP handler serving embedded `web/dist/` with SPA fallback routing
- Makefile with `make web`, `make build`, `make test`, `make clean` targets

---

## Phase 3 — Agent Generation Engine (2026-06-06)

### Added
- `internal/agent` package: LLM-driven agent synthesis from knowledge clusters
- Agent schema: ID, Version, Domain, SystemPrompt, Instructions, AntiPatterns, SourceRefs, Status, ChangeLog
- Monotonic agent versioning with full history preserved in `agent_versions` table
- Diff engine: compare agent versions, produce human-readable changelog
- Draft / published states with curator approval gate
- Export formats: Claude subagent `.md`, plain `.txt`, structured `.json`
- SQLite `agents` and `agent_versions` schema additions
- MCP tools: `agent_get`, `agent_list`, `agent_publish`, `agent_export`
- MCP resource: `agents://generated`, `agents://domain/{name}`
- MCP prompt: `use_agent/{domain}`
- Pipeline `WithAgentGeneration` hook: generates agents after cluster summarization
- `StoreAgentVersion` uses `INSERT OR IGNORE` for UNIQUE safety

---

## Phase 2 — Knowledge Analysis Pipeline (2026-06-05)

### Added
- Background pipeline goroutine: configurable trigger (entry count threshold + time interval)
- Cosine similarity clustering over stored embeddings
- Quality scoring formula: `(rating × usage_count) + coherence + specificity`
- LLM cluster summarization via claude-haiku-4-5
- Auto-tagging: LLM-suggested domain and tag improvements
- Coverage gap detection: domains below entry threshold surfaced
- Versioned dataset snapshots persisted to storage
- SQLite schema additions: `clusters`, `pipeline_runs`, `dataset_snapshots`
- MCP tools: `cluster_list`, `analysis_status`
- Anthropic raw HTTP client with rate limiting and retry logic
- `AnalysisStore` interface for all pipeline storage operations
- Near-duplicate detection via clustering (deduplication deferred to Phase 5 curator workflow)

---

## Phase 1 — Core MCP + Storage (2026-06-05)

### Added
- Go module scaffold: `github.com/dsandor/memory`
- SQLite + sqlite-vec database with schema: `entries`, `embeddings`, `users`, `teams`
- Embedding service abstraction with Ollama provider
- MCP server over stdio transport using mark3labs/mcp-go
- MCP tools: `knowledge_store`, `knowledge_get`, `knowledge_list`, `knowledge_delete`
- Semantic search: vector similarity top-K via sqlite-vec
- Config loading from environment variables with validation
- `StoreEntry` returns assigned UUID on create without a follow-up query
- Unit tests for storage and embedding layers
```

### 8.2 Update STATE.md

**File:** `/Users/dsandor/Projects/memory/.planning/STATE.md`

Changes:
- Set `Phase:` to `6`
- Set `Status:` to `in-progress`
- Set `Last updated:` to `2026-06-07`
- Update Phase 6 row: change `pending` to `in-progress`, add plan link `2026-06-07-phase6-polish-devex.md`
- Add a Phase 6 bullet in the Notes section summarizing what is being implemented

### 8.3 Update ROADMAP.md

**File:** `/Users/dsandor/Projects/memory/.planning/ROADMAP.md`

In the Status table at the bottom, change Phase 6 status from `pending` to `in-progress`.

Checkbox steps:
- [ ] Create `/Users/dsandor/Projects/memory/CHANGELOG.md`
- [ ] Edit `.planning/STATE.md`: update phase/status/date, Phase 6 table row and notes
- [ ] Edit `.planning/ROADMAP.md`: update Phase 6 status in the table

---

## Dependency Order and Parallelism

Tasks can be executed in this order to minimize blocking:

```
Task 1 (slog)          ──► Task 2 (health) depends on Ping methods from Task 1 work context
Task 2 (health)        ──► requires Task 1 complete (slog used inside health handler)
Task 3 (graceful shutdown)  ──► can run in parallel with Task 2
Task 4 (Dockerfile)    ──► can start after Task 1 (needs binary to build cleanly)
Task 5 (compose/.env)  ──► depends only on Dockerfile existing
Task 6 (seed.py)       ──► fully independent, can be done any time
Task 7 (README)        ──► best done after Tasks 4-6 so docker/seed commands are verified
Task 8 (CHANGELOG/state) ──► final task; update after all others are complete
```

Recommended execution sequence: 6, 3, 1, 2, 4, 5, 7, 8.

---

## File Index

| Task | Files Created or Modified |
|---|---|
| 1 | `internal/config/config.go`, `cmd/server/main.go`, `internal/config/config_test.go`, all internal packages with `log.*` calls |
| 2 | `internal/storage/storage.go`, `internal/storage/sqlite.go`, `internal/embedding/ollama.go`, `internal/pipeline/pipeline.go`, `internal/web/server.go`, `Makefile` |
| 3 | `cmd/server/main.go`, `internal/pipeline/pipeline.go` |
| 4 | `Dockerfile` (new) |
| 5 | `docker-compose.yml` (new), `.env.example` (new) |
| 6 | `scripts/seed.py` (new) |
| 7 | `README.md` (new) |
| 8 | `CHANGELOG.md` (new), `.planning/STATE.md`, `.planning/ROADMAP.md` |
