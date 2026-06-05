# Tribal Knowledge MCP Server — Roadmap

## Project
A self-improving team knowledge engine: single Go binary that captures tribal knowledge, continuously analyzes and reorganizes it, generates evolving specialized AI agents per domain, and exposes everything through an embedded web UI and MCP protocol.

---

## Phases

### Phase 1 — Core MCP + Storage
**Goal:** Working Go binary with MCP server and knowledge CRUD — the minimal foundation everything else builds on.

**Deliverables:**
- Go module scaffold with `cmd/server/main.go` entry point
- SQLite + sqlite-vec database with schema: `entries`, `embeddings`, `users`, `teams`
- Embedding service abstraction (Ollama provider for v1)
- MCP server over stdio transport (mark3labs/mcp-go)
- MCP tools: `knowledge_store`, `knowledge_get`, `knowledge_list`, `knowledge_delete`
- Basic semantic search (vector similarity, top-K)
- Config loading from env vars with JSON schema validation
- Go `testing` unit tests for storage and embedding layers

**Exit criteria:** Can connect Claude Desktop to the binary via MCP config, store a knowledge entry, and retrieve it by semantic search.

---

### Phase 2 — Knowledge Analysis Pipeline
**Goal:** Background goroutine pipeline that continuously clusters, deduplicates, scores, and summarizes stored knowledge — producing versioned dataset snapshots.

**Deliverables:**
- Pipeline runner: configurable trigger (entry count threshold + time interval)
- Clustering: cosine similarity grouping over embeddings
- Deduplication: near-duplicate detection + LLM-assisted merge (claude-haiku-4-5)
- Quality scoring: `(rating × usage_count) + coherence + specificity` formula
- LLM cluster summarization (claude-haiku-4-5)
- Auto-tagging: LLM suggests improved domain/tag assignments
- Coverage gap detection: domains with < threshold entries surfaced
- Versioned dataset snapshots persisted to storage
- Schema additions: `clusters`, `pipeline_runs`, `dataset_snapshots`
- MCP tools: `cluster_list`, `analysis_status`
- Anthropic SDK integration with rate limiting and retry

**Exit criteria:** After storing 10+ entries, pipeline runs and produces clusters with LLM summaries; snapshot is queryable; gaps are detected.

---

### Phase 3 — Agent Generation Engine
**Goal:** LLM synthesizes specialized agent definitions from knowledge clusters; agents are versioned, carry changelogs, and are exportable in three formats.

**Deliverables:**
- Agent generation: LLM call per cluster → system prompt synthesis (claude-sonnet-4-6)
- Agent schema: `ID`, `Version`, `Domain`, `SystemPrompt`, `Instructions`, `AntiPatterns`, `SourceRefs`, `Status`, `ChangeLog`
- Agent versioning: monotonic versions, full history preserved
- Diff engine: compare agent versions, generate human-readable changelog
- Draft / published states: curators approve before MCP serving
- Export formats:
  - Claude subagent `.md` (YAML frontmatter + system prompt)
  - System prompt `.txt` (plain text, any LLM)
  - `.json` (full structured config with all knowledge refs)
- Schema additions: `agents`, `agent_versions`
- MCP tools: `agent_get`, `agent_list`
- MCP resources: `agents://generated`, `agents://domain/{name}`
- MCP prompt: `use_agent/{domain}`

**Exit criteria:** After pipeline runs on a cluster, an agent is generated, versioned, diffed, and exportable in all three formats. Agent is retrievable via MCP.

---

### Phase 4 — Embedded Web UI
**Goal:** React SPA compiled by Vite and embedded in the Go binary via `//go:embed` — full visibility into knowledge, clusters, datasets, and agents with download flows.

**Deliverables:**
- React + TypeScript + Vite project in `web/` directory
- shadcn/ui + Tailwind dark theme
- Pages: Dashboard, Knowledge Browser, Clusters, Datasets, Agents, Agent Detail
- Knowledge Browser: paginated list, search, filter by type/domain/cluster/rating, entry detail view, inline rating
- Clusters view: cluster list with entry counts and LLM summaries
- Datasets view: versioned snapshot history, before/after pipeline diffs
- Agents view: agent list with version history, status badges
- Agent Detail: full definition, source knowledge refs, version diff viewer, approve/reject draft
- Download flows: single agent (3 formats), bulk ZIP
- Dataset export: JSON, CSV
- `go:embed dist/` integration — `web/dist/` served by Go HTTP handler
- Vite build step documented in Makefile / build script

**Exit criteria:** `go build` produces a single binary; opening `http://localhost:8080` shows the dashboard with live data; all download flows produce valid files.

---

### Phase 5 — REST API + Analytics + Team Model
**Goal:** Full REST API backing the web UI, team/user scoping for all data, analytics views, pipeline manual trigger, and settings management.

**Deliverables:**
- REST API handlers (Go `net/http` + chi router):
  - `/api/knowledge` — full CRUD, paginated list
  - `/api/clusters` — list, get with entries, get summary
  - `/api/agents` — list, get, download (format param), domain/latest
  - `/api/pipeline/status` — run history; `POST /trigger` for manual run
  - `/api/analytics/usage`, `/gaps`, `/contributions`
  - `/api/settings` — GET/PUT team config
- Team and user scoping: all knowledge/agents scoped to `team_id`
- Role-based access: `member`, `curator`, `admin` enforced on mutations
- Basic auth (API key per team for v1)
- Analytics views in web UI: usage heatmaps, top entries, domain coverage gaps, contribution leaderboard
- Settings page: domain taxonomy, pipeline schedule, embedding provider, agent generation thresholds
- Schema additions: `api_keys`, role column on `users`
- MCP tools: `prompt_enhance`, `prompt_suggest` (depends on Phase 2 clusters + Phase 3 agents)
- MCP resources: `knowledge://team/top`, `knowledge://team/recent`, `knowledge://domain/{name}`, `knowledge://cluster/{id}`
- MCP prompt: `enhance_with_context`

**Exit criteria:** Web UI is fully functional with live REST data; team scoping enforced; analytics pages render; pipeline can be manually triggered from UI; prompt enhancement tools work via MCP.

---

### Phase 6 — Polish & Developer Experience
**Goal:** Production-ready: HTTP-SSE MCP transport, structured logging, health checks, Docker, seed data, onboarding, and full documentation.

**Deliverables:**
- HTTP-SSE MCP transport (remote client support)
- Structured logging (zerolog or slog) with configurable level
- `/health` endpoint: storage ping, embedding service ping, pipeline status
- Graceful shutdown: drain in-flight requests, complete pipeline stage
- Docker: `Dockerfile` (multi-stage: `node` build → `golang` build → `scratch` runtime)
- `docker-compose.yml`: server + optional Ollama sidecar
- Seed data script: realistic example entries across 3+ domains
- Onboarding wizard page in web UI (first-run detection, guided setup)
- PostgreSQL + pgvector storage adapter (same interface as SQLite)
- README: installation, MCP config snippets for Claude Desktop / Cursor / Zed
- Config reference: all env vars documented
- CHANGELOG.md initialized

**Exit criteria:** `docker compose up` starts the full stack; README onboards a new user in < 10 minutes; binary passes health check; all tests pass.

---

## Sequence

```
Phase 1 ──► Phase 2 ──► Phase 3 ──► Phase 4 ──► Phase 5 ──► Phase 6
  MCP          Pipeline    Agents      Web UI      REST API    Polish
  + Storage                                        + Team
```

Each phase produces a working, testable increment. Phases 1–3 are backend-only. Phase 4 adds the UI shell. Phase 5 wires everything together. Phase 6 hardens for production.

---

## Status

| Phase | Name | Status |
|-------|------|--------|
| 1 | Core MCP + Storage | `pending` |
| 2 | Knowledge Analysis Pipeline | `pending` |
| 3 | Agent Generation Engine | `pending` |
| 4 | Embedded Web UI | `pending` |
| 5 | REST API + Analytics + Team Model | `pending` |
| 6 | Polish & Developer Experience | `pending` |

---

*Created: 2026-06-05*
