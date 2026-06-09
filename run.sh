#!/usr/bin/env bash
# run.sh — start PostgreSQL in Docker, then launch the MCP server via go run.
#
# Claude Desktop config (~/.config/claude/claude_desktop_config.json):
#
#   {
#     "mcpServers": {
#       "tribal-knowledge": {
#         "command": "/path/to/memory/run.sh",
#         "env": {
#           "ANTHROPIC_API_KEY": "sk-ant-...",
#           "SUPERADMIN_KEY":    "change-me-in-production"
#         }
#       }
#     }
#   }
#
# All variables below can be overridden from the environment or the MCP env block.

set -euo pipefail

# ── Resolve project root (works regardless of CWD when Claude Desktop calls this)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Configurable defaults ────────────────────────────────────────────────────
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-memory-postgres}"
POSTGRES_USER="${POSTGRES_USER:-memory}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-memory}"
POSTGRES_DB="${POSTGRES_DB:-memory}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
DB_DIR="${DB_DIR:-${SCRIPT_DIR}/db}"

# Server settings — override from env or MCP env block
LOG_LEVEL="${LOG_LEVEL:-info}"
HTTP_ADDR="${HTTP_ADDR:-:8080}"
DEV_BYPASS_AUTH="${DEV_BYPASS_AUTH:-false}"
OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434}"
OLLAMA_MODEL="${OLLAMA_MODEL:-nomic-embed-text}"

# ── Ensure Docker is available ───────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
    echo "run.sh: docker not found — install Docker Desktop and retry" >&2
    exit 1
fi

# ── Start PostgreSQL container ───────────────────────────────────────────────
if ! docker inspect "${POSTGRES_CONTAINER}" &>/dev/null; then
    # Container does not exist — create it.
    mkdir -p "${DB_DIR}"
    echo "run.sh: creating postgres container '${POSTGRES_CONTAINER}' (data → ${DB_DIR})" >&2
    docker run -d \
        --name  "${POSTGRES_CONTAINER}" \
        -e      POSTGRES_USER="${POSTGRES_USER}" \
        -e      POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
        -e      POSTGRES_DB="${POSTGRES_DB}" \
        -v      "${DB_DIR}:/var/lib/postgresql/data" \
        -p      "${POSTGRES_PORT}:5432" \
        pgvector/pgvector:pg17 \
        >/dev/null
else
    # Container exists — ensure it is running (may have been stopped).
    CONTAINER_STATE="$(docker inspect -f '{{.State.Status}}' "${POSTGRES_CONTAINER}")"
    if [ "${CONTAINER_STATE}" != "running" ]; then
        echo "run.sh: starting existing postgres container '${POSTGRES_CONTAINER}'" >&2
        docker start "${POSTGRES_CONTAINER}" >/dev/null
    fi
fi

# ── Wait for Postgres to accept connections ──────────────────────────────────
echo "run.sh: waiting for postgres to be ready..." >&2
TRIES=0
until docker exec "${POSTGRES_CONTAINER}" \
        pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" &>/dev/null; do
    TRIES=$((TRIES + 1))
    if [ "${TRIES}" -ge 30 ]; then
        echo "run.sh: postgres did not become ready after 30s — aborting" >&2
        exit 1
    fi
    sleep 1
done
echo "run.sh: postgres ready" >&2

# ── Export environment for the Go server ────────────────────────────────────
export DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"
export HTTP_ADDR
export LOG_LEVEL
export DEV_BYPASS_AUTH
export OLLAMA_URL
export OLLAMA_MODEL
export CGO_ENABLED=1

# ANTHROPIC_API_KEY, SUPERADMIN_KEY, AGENT_MODEL, ANTHROPIC_MODEL,
# MCP_HTTP_ADDR, MCP_HTTP_PATH, EMBEDDING_DIM — pass through from caller env.

# ── Launch the MCP server (replaces this shell process) ─────────────────────
cd "${SCRIPT_DIR}"
exec go run ./cmd/server
