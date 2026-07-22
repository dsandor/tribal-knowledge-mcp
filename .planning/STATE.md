# Project State

## Current

- **Phase:** 11
- **Status:** complete
- **Last updated:** 2026-07-21

## Phase Progress

| Phase | Name | Status | Plan |
|-------|------|--------|------|
| 1 | Core MCP + Storage | `complete` | [2026-06-05-phase1-core-mcp-storage.md](../docs/superpowers/plans/2026-06-05-phase1-core-mcp-storage.md) |
| 2 | Knowledge Analysis Pipeline | `complete` | [2026-06-05-phase2-knowledge-analysis-pipeline.md](../docs/superpowers/plans/2026-06-05-phase2-knowledge-analysis-pipeline.md) |
| 3 | Agent Generation Engine | `complete` | [2026-06-06-phase3-agent-generation-engine.md](../docs/superpowers/plans/2026-06-06-phase3-agent-generation-engine.md) |
| 4 | Embedded Web UI | `complete` | [2026-06-06-phase4-embedded-web-ui.md](../docs/superpowers/plans/2026-06-06-phase4-embedded-web-ui.md) |
| 5 | REST API + Analytics + Team Model | `complete` | [2026-06-07-phase5-rest-api-analytics-team-design.md](../docs/superpowers/specs/2026-06-07-phase5-rest-api-analytics-team-design.md) |
| 6 | Polish & Developer Experience | `complete` | [2026-06-07-phase6-polish-devex.md](../docs/superpowers/plans/2026-06-07-phase6-polish-devex.md) |
| 7 | PostgreSQL Adapter + Local Dev + Onboarding | `complete` | [2026-06-07-phase7-postgres-adapter.md](../docs/superpowers/plans/2026-06-07-phase7-postgres-adapter.md) |
| 8 | Prompt Feedback & Active Learning | `complete` | — |
| 9 | Bulk Import, Hybrid Search & Production Hardening | `complete` | — |
| 10 | Knowledge Detail Editing, Curator Batch Actions & Search Highlighting | `complete` | — |
| 11 | TODO Subsystem | `complete` | [2026-07-20-todo-subsystem-design.md](../docs/superpowers/specs/2026-07-20-todo-subsystem-design.md) / [2026-07-20-todo-subsystem.md](../docs/superpowers/plans/2026-07-20-todo-subsystem.md) |

## Notes

- Phase 1 complete: working MCP binary with knowledge CRUD and sqlite-vec semantic search
- StoreEntry returns assigned UUID — callers receive the ID on create without a follow-up query
- Phase 2 complete: AnalysisStore interface, schema extensions (clusters/pipeline_runs/dataset_snapshots), Anthropic LLM client (raw HTTP), cosine-similarity clustering, quality scoring, pipeline orchestrator, cluster_list and analysis_status MCP tools; all tests pass
- Deduplication deferred to Phase 5 curator workflow; near-duplicates surface via clustering
- Phase 3 complete: Agent Generation Engine — `internal/agent` package (Generate/Diff/Export), AgentStore interface + SQLite impl, pipeline WithAgentGeneration hook, 4 MCP tools (agent_list/get/publish/export) + use_agent prompt, AgentModel config field; all tests pass (~65 total)
- All mcp-go v0.54.1 prompt API types compile cleanly (AddPrompt, GetPromptResult, PromptMessage, WithPromptCapabilities etc.)
- Published agents preserve status across pipeline re-runs; StoreAgentVersion uses INSERT OR IGNORE for UNIQUE safety
- Phase 4 complete: embedded React SPA (Dashboard, Knowledge Browser, Knowledge Detail, Clusters, Datasets, Agents, Agent Detail), Go HTTP server with 14 REST endpoints, Makefile with make web/build/test/clean targets
- Phase 5 design approved: full multi-tenant federation, chi router, OIDC+local auth, superadmin/admin/curator/member roles, API key (team+per-user), team scoping on all data, analytics (usage/gaps/contributions), curator approval queue, MCP HTTP/SSE remote transport, knowledge_search + knowledge_rate + prompt_suggest MCP tools, agents:// resources
- Phase 5 complete: full REST API (chi router, 14+ endpoints), analytics handlers (usage/gaps/contributions), settings GET/PUT, auth middleware (API key + role enforcement), team scoping, React analytics + settings pages wired to live REST data; all Go tests pass, Go binary builds clean, React/Vite production build clean (308 kB JS, 22 kB CSS)
- Phase 6 adds structured logging, enhanced /health, graceful shutdown, Docker packaging, seed script, and README
- Phase 6 complete: slog JSON logging (LOG_LEVEL), /health with per-component JSON status, graceful shutdown (15s drain + pipeline stageDone), multi-stage Dockerfile (CGO/sqlite-vec), docker-compose.yml with ollama profile, .env.example, scripts/seed.py (9 entries, 3 domains), README.md (9 sections), CHANGELOG.md; all 9 Go test packages pass, Vite production build clean
- Phase 7 complete: PostgreSQL + pgvector storage adapter — PostgresStore implements AgentStore + TeamStore + RuleStore (~55 methods across postgres.go, postgres_analysis.go, postgres_agents.go, postgres_teams.go, postgres_rules.go); DATABASE_URL env var selects Postgres vs SQLite at startup; all 9 Go test packages pass
- run.sh: starts Docker-managed PostgreSQL (pgvector/pgvector:pg17), waits for pg_isready, exports DATABASE_URL + all server env vars, then `exec go run ./cmd/server` — usable directly as a Claude Desktop MCP command
- docker-compose.yml: postgres + server (depends_on healthy) + ollama (optional profile); DB_DIR volume mount; DATABASE_URL injected automatically
- Makefile deploy target: `make image` builds/tags locally; `make deploy` pushes to registry (REGISTRY/IMAGE/VERSION variables)
- Onboarding wizard: 4-step React UI at /onboarding — Welcome → Create Team → Create API Key (with copy) → Seed Example Data (9 entries, progress bar); Dashboard auto-redirects to /onboarding when knowledge list is empty; Vite build clean
- Phase 10 complete: Knowledge detail inline edit (title/content/type/domain/tags/description) + delete with confirmation + similar entries panel; PendingQueue rewritten with dark theme, checkbox multi-select, bulk approve/reject, skeleton loading; POST /api/knowledge/batch-approve and batch-reject backend endpoints; search snippet highlighting (getSnippet centers on match, Highlight marks terms in amber) + mode badge on Knowledge Browser; Go 9/9 pass, Vite clean
- Security fixes: CSV formula injection (csvSafeCell prefixes =+-@ cells); XFF rate-limit bypass (TRUST_XFF=false default, opt-in with rightmost-entry policy)
- Phase 9 complete: SearchHybrid on Store interface (FTS4/tsvector + vector cosine merge); BulkImport (transaction, title-hash dedup, per-entry error collection); POST /api/knowledge/import (JSON array + multipart CSV); GET /api/knowledge/export (JSON + CSV streaming, domain/type/tag filters); rate-limit middleware (token bucket, RATE_LIMIT_RPS, per-IP, 5m bucket pruning); maxBodySize middleware (1 MB POST/PUT guard); consistent writeError helper (code + message JSON on all 4xx/5xx); Import UI page (JSON paste + drag-and-drop CSV, preview table, result box); Knowledge Browser hybrid/semantic/keyword toggle; Go 9/9 tests pass, Vite clean
- Phase 8 complete: usage_events/outcome_ratings/feed_activity storage tables (SQLite + Postgres); RecordUsage/RecordOutcome/GetTrendingEntries/GetWeakSignalEntries/RecordActivity/ListActivity on Store interface; `knowledge_use` MCP tool; GET /api/knowledge/trending and GET /api/activity REST endpoints; WeakSignalImprovement pipeline stage (haiku rewrites low-rated entries → draft for curator review); Dashboard Trending + Activity Feed widgets (polling 30s); Go build clean, Vite build clean (325 kB JS)
- Phase 11 complete: TODO Subsystem — `TodoStore` interface + SQLite/Postgres implementations (todo_lists/todo_items/todo_external_links/todo_knowledge_refs, team-scoped, cascade delete); 12 MCP tools (todo_lists/todo_list_create/todo_list_update/todo_list_delete/todo_add/todo_get/todo_query/todo_update/todo_complete/todo_delete/todo_link_issue/todo_link_knowledge) + `todos://mine`/`todos://overdue`/`todos://list/{id}` resources + `manage_todos` prompt; REST API under `/api/todo-lists` + `/api/todos` (CRUD, filtered query, complete, external links, knowledge-ref replace) with PATCH-style partial updates; Kanban web UI (board + list view, detail drawer, Knowledge Detail "Related todos" panel, Dashboard "My Todos" widget); security hardening — external-link deletion scoped to (todo_id, link_id) so a link can't be deleted via cross-team ID guessing, knowledge-ref writes validate each entry ID against the caller's team, external link URLs restricted to http(s); Go 16/16 packages pass (556 tests), Vite build clean; REST smoke test (create list → create todo → PATCH status-only → link → knowledge-refs → filtered query → complete → list counts) verified end-to-end against a scratch SQLite DB
