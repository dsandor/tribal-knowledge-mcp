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
- README: installation, MCP config snippets for Claude Desktop / Cursor / Zed
- Config reference: all env vars documented
- CHANGELOG.md initialized

**Exit criteria:** `docker compose up` starts the full stack; README onboards a new user in < 10 minutes; binary passes health check; all tests pass.

---

### Phase 7 — PostgreSQL Adapter + Local Dev + Onboarding
**Goal:** Production-grade storage backend, developer ergonomics for local/Claude Desktop use, and a guided first-run experience in the web UI.

**Deliverables:**
- PostgreSQL + pgvector storage adapter (`PostgresStore`) implementing all storage interfaces
- `DATABASE_URL` env var selects Postgres vs SQLite at startup (no code changes needed)
- `run.sh`: Docker-managed PostgreSQL → `go run ./cmd/server` (usable as Claude Desktop MCP command)
- `docker-compose.yml` updated: `postgres` (pgvector/pgvector:pg17) as default service, `DB_DIR` volume mount
- `make image`: build + tag Docker image locally
- `make deploy`: build, tag, and push to container registry (`REGISTRY`/`IMAGE`/`VERSION` vars)
- Onboarding wizard (4-step React UI at `/onboarding`): Welcome → Create Team → Create API Key → Seed Example Data
- Dashboard auto-redirects to `/onboarding` when knowledge list is empty

**Exit criteria:** `./run.sh` starts PostgreSQL + server cleanly; Claude Desktop config using run.sh works; onboarding wizard completes and lands on Dashboard with seeded entries; `make deploy` pushes a tagged image.

---

### Phase 8 — Prompt Feedback & Active Learning
**Goal:** Close the feedback loop: track which knowledge entries and prompt suggestions are actually used, learn from outcomes, surface higher-signal content, and actively improve prompt suggestions over time.

**Deliverables:**
- **Usage tracking**: record when `prompt_suggest`/`enhance_with_context` results are used by a client (MCP tool `knowledge_use` — called after a suggestion is accepted)
- **Outcome rating**: `knowledge_rate` extended with `outcome` field (1–5: how well the prompt worked in practice)
- **Signal scoring**: weighted score formula updated to incorporate usage frequency + outcome ratings + recency decay
- **Trending entries**: `GET /api/knowledge/trending` — top entries by signal score in last 7/30 days
- **Weak-signal detection**: pipeline stage identifies entries with low outcome ratings; flags for curator review or auto-archives after N failures
- **Prompt A/B tracking**: when multiple suggestions are returned, track which was selected; surface selection rate per entry
- **Improvement suggestions**: LLM pipeline stage rewrites low-rated entries using patterns from high-rated ones in the same domain (claude-haiku-4-5); stores as a new draft version pending curator approval
- **Activity feed API**: `GET /api/activity` — recent stores, ratings, approvals, pipeline events (paginated)
- **Activity feed UI**: new Dashboard widget showing team activity in real time (polling or SSE)
- **Storage**: `usage_events` table (entry_id, user_id, tool, timestamp, selected_index); `outcome_ratings` table; signal_score column on entries
- MCP tool: `knowledge_use` (records acceptance of a suggestion)

**Exit criteria:** After a team uses `prompt_suggest`, accepted suggestions are recorded; low-rated entries are flagged in the curator queue; the pipeline rewrites a low-rated entry and surfaces a draft improvement; activity feed shows live team activity on Dashboard.

---

### Phase 9 — Bulk Import, Hybrid Search & Production Hardening
**Goal:** Let teams seed from existing docs, improve search quality, and make the API safe for production.

**Deliverables:**
- **Bulk import API**: `POST /api/knowledge/import` — accepts JSON array or multipart CSV; deduplication via title hash; returns `{imported, skipped, errors}`
- **Import UI page**: drag-and-drop file upload (JSON/CSV), column mapping for CSV, domain assignment, preview table, submit with progress
- **Hybrid search**: SQLite FTS5 + vector cosine combined score; PostgreSQL `tsvector` + pgvector combined score; `?q=text&mode=hybrid|semantic|keyword` on `GET /api/knowledge`
- **Export endpoint**: `GET /api/knowledge/export?format=csv|json&domain=...&tag=...` — streams all matching entries, no pagination cap
- **Rate limiting**: token-bucket middleware (configurable via `RATE_LIMIT_RPS` env var, default 60 req/min per IP); returns 429 with `Retry-After` header
- **Request size guard**: 1 MB body limit on all POST/PUT handlers
- **Improved error responses**: consistent `{"error":"...", "code":"..."}` JSON on 4xx/5xx; no stack traces leaked to clients

**Exit criteria:** A team can import a 100-entry CSV in one API call; hybrid search returns more relevant results than pure vector for short keyword queries; API rejects oversized payloads; rate limiter trips at configured threshold; export produces a valid CSV with all fields.

---

### Phase 10 — Knowledge Detail Editing, Curator Batch Actions & Search Highlighting
**Goal:** Make the day-to-day knowledge management experience complete — curators can edit entries in-place, process the queue in bulk, and search results show why each entry matched.

**Deliverables:**
- Knowledge detail page: inline edit mode (title, content, type, domain, tags), save/cancel, delete with confirmation dialog
- `PUT /api/knowledge/:id` and `DELETE /api/knowledge/:id` backend endpoints (verify/add)
- PendingQueue: checkbox multi-select, "Approve selected" / "Reject selected" bulk actions, dark-theme polish
- `POST /api/knowledge/batch-approve` and `POST /api/knowledge/batch-reject` backend endpoints
- Search snippet highlighting: matched query terms highlighted in search results on Knowledge Browser
- "Similar entries" panel on Knowledge Detail (top 3 semantically similar, links to detail pages)

**Exit criteria:** A curator can edit, delete, and bulk-process pending entries; search results highlight matching terms; detail page shows related entries; all pages use consistent dark theme.

---

## Sequence

```
Phase 1 ──► Phase 2 ──► Phase 3 ──► Phase 4 ──► Phase 5 ──► Phase 6 ──► Phase 7 ──► Phase 8 ──► Phase 9
  MCP          Pipeline    Agents      Web UI      REST API    Polish      Postgres    Feedback    Import
  + Storage                                        + Team                  + DevEx     + Learning  + Search
```

Each phase produces a working, testable increment. Phases 1–3 are backend-only. Phase 4 adds the UI shell. Phase 5 wires everything together. Phase 6 hardens for production. Phase 7 adds production-grade storage and developer ergonomics. Phase 8 closes the learning loop.

---

## Status

| Phase | Name | Status |
|-------|------|--------|
| 1 | Core MCP + Storage | `complete` |
| 2 | Knowledge Analysis Pipeline | `complete` |
| 3 | Agent Generation Engine | `complete` |
| 4 | Embedded Web UI | `complete` |
| 5 | REST API + Analytics + Team Model | `complete` |
| 6 | Polish & Developer Experience | `complete` |
| 7 | PostgreSQL Adapter + Local Dev + Onboarding | `complete` |
| 8 | Prompt Feedback & Active Learning | `complete` |
| 9 | Bulk Import, Hybrid Search & Production Hardening | `planned` |

---

*Created: 2026-06-05*
