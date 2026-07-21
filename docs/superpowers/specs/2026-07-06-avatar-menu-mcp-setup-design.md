# Avatar Menu: Inline API Keys + MCP Setup Dialog

**Date:** 2026-07-06
**Status:** Approved

## Goal

Extend the existing avatar menu (`web/src/components/UserMenu.tsx`, bottom of the left sidebar) so a user can, without leaving the menu:

1. See their teams and which is active (already exists — unchanged).
2. Jump to visibility settings (already exists — unchanged).
3. See their API keys inline (name + masked value + copy button) with a link to full management.
4. Open an **MCP Setup** dialog with copyable snippets for connecting Claude Code CLI and Claude Desktop to this server's remote MCP endpoint, with their real API key and the server's real MCP URL baked in.

## Non-goals

- No key create/revoke from the menu (stays on `/api-keys`).
- No stdio/local-binary Claude Desktop instructions (single-user mode; defeats the team-server purpose).
- No refactor of existing copy-button call sites (new shared util used by new code only).

## Current state (verified)

- `UserMenu.tsx` already renders: profile header, active-team switcher (`getMyTeams()`, check mark on active), "My Visibility" → `/my-visibility`, "My API Keys" → `/api-keys`, Sign Out.
- `GET /api/api-keys` (`internal/web/admin_handlers.go:316`, `handleListAPIKeys`) returns all keys for the **active team** (`tc.TeamID`), each with `id, team_id, user_id, key_type ('team'|'user'), name, role, raw_key, created_at`. `raw_key` is the full plaintext key (retained by design); empty string for keys created before retention.
- Remote MCP: Streamable HTTP at `MCP_HTTP_ADDR` (env; empty = disabled) + `MCP_HTTP_PATH` (default `/mcp`), config fields `cfg.MCPHTTPAddr` / `cfg.MCPHTTPPath` (`internal/config/config.go:33-34,209-210`). Auth is `Authorization: Bearer <key>`.
- The web `Server` struct (`internal/web/server.go:34`) does not currently know the MCP addr/path.
- Clipboard copy logic is duplicated in 4 places (e.g. `web/src/components/ui/markdown-view.tsx:18-32`); no shared util.

## Design

### 1. Shared clipboard util — `web/src/lib/clipboard.ts` (new)

```ts
export async function copyToClipboard(text: string): Promise<boolean>
```

`navigator.clipboard.writeText` with the `document.execCommand('copy')` textarea fallback (same logic as `markdown-view.tsx:18-32`). Used by all new code below. Existing call sites are left untouched.

### 2. Backend — `GET /api/mcp-info` (new)

- New fields on `web.Server`: `mcpHTTPAddr, mcpHTTPPath string`, set via a chained option `WithMCPInfo(addr, path string) *Server` (matches the existing builder style used in `cmd/server/main.go:193`). Wire it in `main.go` from `cfg.MCPHTTPAddr` / `cfg.MCPHTTPPath`.
- Handler (new file `internal/web/mcp_info_handlers.go`), registered in the authenticated route group next to `/api/me`:

```json
{ "http_enabled": true, "url": "http://myhost:8081/mcp" }
```

- `http_enabled` = `mcpHTTPAddr != ""`. When disabled, `url` is `""`.
- URL construction: scheme from `X-Forwarded-Proto` (only when `s.trustXFF`) else `r.TLS != nil ? "https" : "http"`; hostname from `r.Host` (strip its port); port from `mcpHTTPAddr` (e.g. `":8081"` or `"0.0.0.0:8081"` → `8081`); path = `mcpHTTPPath`.
- Extract a pure helper `buildMCPURL(scheme, requestHost, mcpAddr, mcpPath string) string` and unit-test it (addr forms `:8081`, `0.0.0.0:8081`, host with/without port).

### 3. Frontend API — `web/src/lib/api.ts`

```ts
export interface MCPInfo { http_enabled: boolean; url: string }
export async function getMCPInfo(): Promise<MCPInfo>  // GET /api/mcp-info
```

### 4. `UserMenu.tsx` — API Keys section

Between the team switcher and the links section:

- On first menu open (lazy, not on mount), fetch `listAPIKeys()`; hold in state. Silent failure → section shows nothing extra.
- Section header caption `API KEYS` (same style as `ACTIVE TEAM`).
- Rows: the caller's own keys first (`key_type === 'user' && user_id === me.user_id`), then team keys if the user has no personal keys. Cap at 3 rows. Each row: key name, masked value (`tk_••••` + last 4 chars of `raw_key`), copy icon button copying the full `raw_key`. Keys with empty `raw_key` render the name + `••••` with no copy button.
- Footer item **"Manage keys →"** replaces the current "My API Keys" item (same `navigate('/api-keys')`).
- If zero keys: single item "No API keys yet — create one" → `/api-keys`.
- Copy success shows a `Snackbar` ("Copied") rendered by `UserMenu`; also used by the dialog.

### 5. `MCPSetupDialog.tsx` (new component)

Opened from a new **"MCP Setup"** menu item (plug icon) placed with the other links. Props: `open`, `onClose`, `keys` (the already-fetched key list), `me`, and an `onCopied` callback for the shared snackbar.

- On open, fetch `getMCPInfo()` once.
- **Key selection:** default = first user-type key owned by the caller, else first team key, else none. When more than one key is available, a small MUI `Select` ("API key: <name>") chooses which key is embedded; snippets re-render live. When none, snippets show `<your-api-key>` and an `Alert` links to `/api-keys`.
- **Server URL:** from `mcp-info.url`. If `http_enabled` is false, show a warning `Alert` — "Remote MCP is not enabled on this server. Set `MCP_HTTP_ADDR` (e.g. `:8081`) and restart." — and render snippets with `<server-url>` placeholder.
- **Tabs** (MUI `Tabs`): **Claude Code** and **Claude Desktop**. Each tab: one-line instruction, dark `<pre>` code block (styled like `markdown-view.tsx`'s code blocks), copy icon button top-right of the block.

Claude Code snippet:

```
claude mcp add --transport http tribal-knowledge \
  http://myhost:8081/mcp \
  --header "Authorization: Bearer tk_abc123..."
```

Claude Desktop snippet (uses `mcp-remote`; the env-var form avoids Claude Desktop's known space-in-args bug). Above the block, note the config file locations (macOS `~/Library/Application Support/Claude/claude_desktop_config.json`, Windows `%APPDATA%\Claude\claude_desktop_config.json`) and "restart Claude Desktop after saving". Below, note it requires Node.js (`npx`):

```json
{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://myhost:8081/mcp", "--header", "Authorization:${TK_AUTH}"],
      "env": { "TK_AUTH": "Bearer tk_abc123..." }
    }
  }
}
```

A private `CodeSnippet` subcomponent (pre block + copy button + copied-state flash) lives in the dialog file; not exported.

### 6. Error handling summary

| Failure | Behavior |
|---|---|
| `listAPIKeys()` fails | Menu shows no key rows; "Manage keys →" still present |
| `getMCPInfo()` fails / 404 | Dialog shows `<server-url>` placeholder + neutral note |
| MCP HTTP disabled | Warning alert + placeholder URL in snippets |
| No keys | `<your-api-key>` placeholder + link to create one |
| Clipboard blocked | `copyToClipboard` returns false → snackbar "Copy failed" |

### 7. Post-review amendment (2026-07-06)

Final review found `GET /api/api-keys` is **admin-gated** (`RequireAdmin`), contradicting the "Current state" claim above — the design as written was inert for member/curator roles. Amended implementation:

- New member-group endpoint `GET /api/me/api-keys` (`internal/web/me_keys_handlers.go`) returns only the caller's own user-type keys, plus team keys when the caller is admin-or-above. The menu and dialog use it via `listMyAPIKeys()`; the admin page keeps `listAPIKeys()`.
- `UserMenu` key fetch is tri-state (`idle/loading/loaded/error`): no false "No API keys" flash while loading, and a failed fetch retries on next menu open. Empty state renders only after a successful zero-key response.
- Role-aware UI: non-admins get "ask a team admin" copy instead of dead-end links to the admin-only `/api-keys` page; "Manage keys →" is admin-only. `MCPSetupDialog` gained an optional `canManageKeys` prop for the same purpose.
- `handleMCPInfo` takes only the first comma-separated token of `X-Forwarded-Proto`.

### 8. Testing / verification

- Go: `go build ./...`; new unit test for `buildMCPURL` in `internal/web`; `go test ./internal/web/...`.
- Web: `npm run build` must pass (per project rule, required before claiming completion).
- Manual: menu renders keys, copy works, dialog tabs render both snippets with real key + URL.
- **No git commits** (standing user rule).
