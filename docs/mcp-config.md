# Connecting Claude Desktop to the Tribal Knowledge MCP Server

## Build

CGO is required. On macOS, install Xcode command-line tools first:
```bash
xcode-select --install
```

Then build:
```bash
CGO_ENABLED=1 go build -o tribal-knowledge ./cmd/server/
```

## Claude Desktop Config

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "/absolute/path/to/tribal-knowledge",
      "env": {
        "DATABASE_PATH": "/Users/you/tribal-knowledge.db",
        "OLLAMA_URL": "http://localhost:11434",
        "OLLAMA_MODEL": "nomic-embed-text",
        "TEAM_ID": "my-team"
      }
    }
  }
}
```

Replace `/absolute/path/to/tribal-knowledge` with the actual build output path.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_PATH` | `knowledge.db` | SQLite database file path (created on first run) |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama server base URL |
| `OLLAMA_MODEL` | `nomic-embed-text` | Embedding model (must produce `EMBEDDING_DIM`-dimensional vectors) |
| `TEAM_ID` | `default` | Team identifier (metadata only in Phase 1) |
| `EMBEDDING_DIM` | `768` | Embedding vector dimensions — must match the model. If changed, delete and recreate the DB. |

## Prerequisites

- Ollama running with the embedding model pulled:
  ```bash
  ollama serve &
  ollama pull nomic-embed-text
  ```
- Go 1.24+ with CGO enabled

## Phase 1 Exit Criteria

Phase 1 is complete when you can:
1. Build the `tribal-knowledge` binary without errors
2. Connect Claude Desktop using the config above (server shows in Claude's tool menu)
3. Call `knowledge_store` to save a prompt entry and receive a response containing the entry ID
4. Call `knowledge_list` and see the stored entry
5. Call `knowledge_get` with the returned ID and receive the full entry JSON
6. Call `knowledge_delete` with the ID and confirm it no longer appears in `knowledge_list`
