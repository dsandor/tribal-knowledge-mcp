# Changelog

All notable changes to this project are documented in this file.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added
- **Per-User Knowledge Visibility & Cross-Team Sharing**
  - Per-user suppression: a user can hide an individual entry, mute an author, or mute a tag/domain so those entries stop influencing *their* searches, `enrich_context`, and lists — without affecting teammates. A user's own authored entries are never hidden by their rules
  - Applies to user-scoped tokens (which now carry the caller's `user_id`) and web sessions; shared team tokens keep the full unfiltered team view
  - New MCP tools `knowledge_hide` / `knowledge_unhide` / `knowledge_mute` / `knowledge_unmute` / `knowledge_visibility`, plus a "My Visibility" web page and Hide / Mute-author actions on the knowledge detail view
  - Cross-team sharing: `knowledge_share` (and a Share action in the UI) mints a single-use link; a recipient on any team opens it and imports a **copy** into their team as a `pending` entry (their curator queue), re-chunked and re-embedded under their team's config with the original author preserved. MCP `knowledge_import` + web `/share/<token>` landing page. Same-team links are just deep links ("already yours, no import needed")
- **Large Knowledge Items via Transparent Multi-Vector Chunking**
  - Knowledge content of any size is accepted; items larger than the embedding model's context window are automatically split into multiple coherent chunks internally, each embedded as its own vector, so nothing is silently truncated/lost. The item remains a single logical entry (one ID, rating, and usage count)
  - New `entry_chunks` + per-chunk vector tables (`vec_chunks` for SQLite, `chunk_embeddings` for Postgres); semantic search runs over chunks and dedupes to one best-scoring result per entry. Existing entries are backfilled to a single chunk on migration
  - Per-team, env-defaulted configuration (Settings UI + env): `EMBEDDING_MAX_TOKENS` (default 8192), `EMBEDDING_CHUNK_OVERLAP` (default 128), `EMBEDDING_MAX_CHUNKS` (default 64); `0` on a team falls back to the env default
  - The `knowledge_store` MCP tool description is rewritten per request to telegraph the requesting team's effective size limit, so clients stop pre-trimming or splitting content
  - `knowledge_store` now reports `chunk_count` and `embedding_max_tokens` in its result
  - Fix: editing a knowledge entry's content now re-chunks and re-embeds it (previously the stored vector went stale after an edit); backup restore also rebuilds chunk vectors
- **Full-Database Backup & Restore with Cross-Engine Migration**
  - Logical full backup of every team and all data — knowledge entries + embeddings, clusters, pipeline runs, dataset snapshots, analysis cache, rules, agents, agent versions, teams, users, API keys, auth config, team settings, and usage/activity history (ephemeral login sessions excluded)
  - Engine-neutral archive format (`tar.gz` of a manifest + per-table JSONL; embeddings as float arrays) that round-trips SQLite ↔ PostgreSQL, enabling SQLite → Postgres (and reverse) migration
  - CLI subcommands on the server binary: `export [--out <file>] [--stdout]` (default `backup-<timestamp>.tar.gz`, written `0600`, with a secrets warning) and `import --in <file> [--force]`
  - Restore is a full replace (truncate + load); a non-empty target is refused unless `--force`. Restore is also refused when the archive's `EMBEDDING_DIM` does not match the target
  - Superadmin-only web "Backup & Restore" section on Settings: download backup + restore upload with a "Force overwrite" checkbox (`GET /api/admin/backup`, `POST /api/admin/restore?force=...`)
  - Security: archives contain secrets (API key hashes, auth config, password hashes) and must be treated as credentials
- **Knowledge Detail Editing, Curator Batch Actions & Search Highlighting (Phase 10)**
  - Knowledge detail page: inline edit mode (title, content, type, domain, tags, description), save/cancel, delete with inline confirmation
  - Similar entries panel on knowledge detail (top 3 semantic matches, hidden when empty)
  - `PUT /api/knowledge/:id` and `DELETE /api/knowledge/:id` endpoints
  - PendingQueue: full dark-theme rewrite, checkbox multi-select, bulk approve/reject, skeleton loading states
  - `POST /api/knowledge/batch-approve` and `POST /api/knowledge/batch-reject` endpoints (curator role)
  - Knowledge Browser: search snippet extraction (centered on first match), query term highlighting in amber, search mode badge
- **Security fixes**
  - CSV formula injection: `csvSafeCell()` prefixes formula-trigger characters (`=`, `+`, `-`, `@`) with a single quote in export output
  - Rate-limit XFF bypass: `X-Forwarded-For` ignored by default; opt-in with `TRUST_XFF=true` (rightmost-entry policy, not leftmost)
- **Bulk Import, Hybrid Search & Production Hardening (Phase 9)**
  - `POST /api/knowledge/import` — accepts JSON array or CSV file; deduplicates by title; returns `{imported, skipped, errors}`
  - `GET /api/knowledge/export` — streams all matching entries as JSON or CSV; supports `domain`, `type`, `tag` filters
  - Hybrid search (`SearchHybrid`): FTS4 (SQLite) / tsvector (PostgreSQL) + vector cosine; `?mode=hybrid|semantic|keyword` on knowledge list endpoint
  - Rate limiting middleware: token-bucket per IP, configurable via `RATE_LIMIT_RPS` (default 60 req/min), returns `429` with `Retry-After` header
  - Request size guard: 1 MB body limit on all POST/PUT handlers
  - Consistent JSON error responses: `{"error":"...", "code":"..."}` on all 4xx/5xx
  - Import UI page: JSON paste with preview table + drag-and-drop CSV upload
  - Knowledge Browser: hybrid/semantic/keyword search mode toggle with contextual hints
- **Prompt Feedback & Active Learning (Phase 8)**
  - `knowledge_use` MCP tool: records when a suggestion is accepted by a client
  - `GET /api/knowledge/trending` — top entries by signal score (7d/30d usage + outcome ratings)
  - `GET /api/activity` — paginated team activity feed (stores, ratings, approvals, pipeline events, improvement drafts)
  - Weak-signal improvement pipeline stage: entries with ≥3 outcome ratings averaging ≤2.5/5 are rewritten by claude-haiku and stored as curator-review drafts
  - Dashboard "Trending This Week" widget (signal score, 7-day usage count)
  - Dashboard "Team Activity" widget (30-second polling, event type indicators, relative timestamps)
  - Storage: `usage_events`, `outcome_ratings`, `feed_activity` tables (SQLite + PostgreSQL)
- Structured logging via `log/slog` with `LOG_LEVEL` env var (debug/info/warn/error)
- Enhanced `/health` endpoint with JSON component status (storage, embedding, pipeline)
- Graceful shutdown: in-flight HTTP drain (15s timeout) + pipeline stage completion
- Multi-stage `Dockerfile` with CGO/sqlite-vec support (debian:bookworm-slim runtime)
- `docker-compose.yml` with PostgreSQL + pgvector as default service, optional Ollama sidecar
- `.env.example` documenting all environment variables
- `scripts/seed.py`: seeds 9 realistic knowledge entries across 3 domains
- `README.md`: installation, MCP config snippets, env var reference, architecture diagram
- **PostgreSQL + pgvector storage adapter** — `PostgresStore` implements all storage interfaces; `DATABASE_URL` env var selects Postgres vs SQLite at startup
- `run.sh`: starts a Docker-managed PostgreSQL container then launches the server via `go run`; usable directly as a Claude Desktop MCP command
- Makefile `image` target: builds and tags Docker image locally
- Makefile `deploy` target: builds, tags, and pushes to a container registry (`REGISTRY`, `IMAGE`, `VERSION` variables)

---

## Phase 5 — REST API, Analytics & Team Model (2026-06-07)

### Added
- chi router with REST endpoints: `/api/knowledge`, `/api/clusters`, `/api/agents`, `/api/pipeline`, `/api/analytics`, `/api/settings`
- Multi-tenant team scoping on all knowledge, agents, and API keys
- Role-based access control: `superadmin`, `admin`, `curator`, `member`
- API key authentication (team-scoped and per-user); SHA-256 hashed, never stored plaintext
- Analytics endpoints: usage heatmaps, domain coverage gaps, contribution leaderboard
- Curator approval queue for agent draft → published workflow
- Settings GET/PUT for team configuration
- HTTP/SSE MCP transport for remote client connections (`MCP_HTTP_ADDR`)
- MCP tools: `knowledge_search`, `knowledge_rate`, `prompt_suggest`, `enhance_with_context`
- MCP resources: `knowledge://team/top`, `knowledge://team/recent`, `knowledge://domain/{name}`, `knowledge://cluster/{id}`
- React analytics and settings pages wired to live REST data
- `DEV_BYPASS_AUTH=true` development bypass that injects superadmin context
- `SUPERADMIN_KEY` env var for production bootstrap of first superadmin API key
- OIDC login flow with CSRF state validation

---

## Phase 4 — Embedded Web UI (2026-06-06)

### Added
- React 18 + TypeScript + Vite SPA compiled and embedded in Go binary via `//go:embed`
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
- MCP resources: `agents://generated`, `agents://domain/{name}`
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
- Near-duplicate detection via clustering

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
- Unit tests for storage and embedding layers
