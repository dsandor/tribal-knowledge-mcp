# Avatar Menu: Inline API Keys + MCP Setup Dialog — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing avatar menu with an inline API-keys section and an "MCP Setup" dialog containing copyable Claude Code CLI and Claude Desktop snippets pre-filled with the user's real API key and the server's real MCP URL.

**Architecture:** One new Go endpoint (`GET /api/mcp-info`) exposes the remote-MCP URL derived from server config + request host. The React side adds a shared clipboard util, an `MCPSetupDialog` component, and extends `UserMenu.tsx` with a lazy-loaded key list and the dialog trigger. Spec: `docs/superpowers/specs/2026-07-06-avatar-menu-mcp-setup-design.md`.

**Tech Stack:** Go (chi router, stdlib only), React 19 + TypeScript + MUI v9 (`sx` prop styling), lucide-react icons, Vite build.

## Global Constraints

- **NEVER run `git commit` or `git push`** — standing user rule; a team member commits manually. Where a normal TDD loop would commit, just verify and move on.
- No new npm or Go dependencies.
- Backend verification: `cd /Users/dsandor/Projects/memory && go build ./... && go test ./internal/web/...` must pass.
- Frontend verification: `cd /Users/dsandor/Projects/memory/web && npm run build` must pass (required before claiming completion).
- Follow existing idioms: MUI components with `sx` (no Tailwind/CSS files), lucide-react icons in `UserMenu.tsx`, `apiFetch` wrapper for all API calls, `writeJSON`/`writeError` helpers in Go handlers.
- The web dev flow serves the built SPA from the Go server; there is no frontend test runner — do not add one.

---

### Task 1: Backend `GET /api/mcp-info`

**Files:**
- Create: `internal/web/mcp_info_handlers.go`
- Create: `internal/web/mcp_info_handlers_test.go`
- Modify: `internal/web/server.go` (struct fields ~line 52, new `WithMCPInfo` option after `WithTrustXFF` ~line 143, route registration after `/api/me/teams` at ~line 223)
- Modify: `cmd/server/main.go` (~line 193, add to the `web.NewServer(...)` builder chain)

**Interfaces:**
- Consumes: `s.trustXFF bool` (existing Server field), `writeJSON(w, v)` helper, `cfg.MCPHTTPAddr` / `cfg.MCPHTTPPath` (`internal/config/config.go:33-34`).
- Produces: `GET /api/mcp-info` (authenticated) returning `{"http_enabled": bool, "url": string}`; pure helper `buildMCPURL(scheme, requestHost, mcpAddr, mcpPath string) string`; builder option `WithMCPInfo(addr, path string) *Server`.

- [ ] **Step 1: Write the failing test**

Create `internal/web/mcp_info_handlers_test.go`:

```go
package web

import "testing"

func TestBuildMCPURL(t *testing.T) {
	cases := []struct {
		name   string
		scheme string
		host   string
		addr   string
		path   string
		want   string
	}{
		{"port-only addr", "http", "myhost:8080", ":8081", "/mcp", "http://myhost:8081/mcp"},
		{"wildcard addr", "http", "myhost", "0.0.0.0:8081", "/mcp", "http://myhost:8081/mcp"},
		{"https scheme", "https", "kb.example.com:443", ":8081", "/mcp", "https://kb.example.com:8081/mcp"},
		{"custom path", "http", "myhost:8080", ":9090", "/mcp/v1", "http://myhost:9090/mcp/v1"},
		{"disabled when addr empty", "http", "myhost:8080", "", "/mcp", ""},
		{"path without leading slash", "http", "myhost", ":8081", "mcp", "http://myhost:8081/mcp"},
		{"empty path defaults to /mcp", "http", "myhost", ":8081", "", "http://myhost:8081/mcp"},
		{"ipv6 request host", "http", "[::1]:8080", ":8081", "/mcp", "http://[::1]:8081/mcp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildMCPURL(c.scheme, c.host, c.addr, c.path)
			if got != c.want {
				t.Errorf("buildMCPURL(%q,%q,%q,%q) = %q, want %q", c.scheme, c.host, c.addr, c.path, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -run TestBuildMCPURL -v`
Expected: FAIL to compile with `undefined: buildMCPURL`

- [ ] **Step 3: Write the implementation**

Create `internal/web/mcp_info_handlers.go`:

```go
package web

import (
	"net"
	"net/http"
	"strings"
)

// buildMCPURL assembles the externally reachable remote-MCP endpoint URL from
// the web request's host (which the browser could reach, so its hostname is a
// good default) and the MCP listener's configured addr/path.
// Returns "" when mcpAddr is empty (remote MCP disabled).
func buildMCPURL(scheme, requestHost, mcpAddr, mcpPath string) string {
	if mcpAddr == "" {
		return ""
	}
	host := requestHost
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		host = h
	}
	port := strings.TrimPrefix(mcpAddr, ":")
	if _, p, err := net.SplitHostPort(mcpAddr); err == nil {
		port = p
	}
	if mcpPath == "" {
		mcpPath = "/mcp"
	} else if !strings.HasPrefix(mcpPath, "/") {
		mcpPath = "/" + mcpPath
	}
	return scheme + "://" + net.JoinHostPort(host, port) + mcpPath
}

// handleMCPInfo reports whether the remote (Streamable HTTP) MCP endpoint is
// enabled and, if so, its URL — used by the UI to render client-setup snippets.
func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.trustXFF {
		if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "" {
			scheme = xfp
		}
	}
	url := buildMCPURL(scheme, r.Host, s.mcpHTTPAddr, s.mcpHTTPPath)
	writeJSON(w, map[string]any{
		"http_enabled": s.mcpHTTPAddr != "",
		"url":          url,
	})
}
```

Note on `net.SplitHostPort(requestHost)`: a bare hostname without a port returns an error, leaving `host = requestHost` unchanged — that's the intended fallback. `net.JoinHostPort` re-brackets IPv6 hosts correctly.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/dsandor/Projects/memory && go test ./internal/web/ -run TestBuildMCPURL -v`
Expected: PASS (all 8 subtests)

- [ ] **Step 5: Add Server fields, builder option, and route**

In `internal/web/server.go`, add two fields to the `Server` struct (after `enrichMaxMemories int` at line 52):

```go
	// mcpHTTPAddr/mcpHTTPPath describe where the remote MCP endpoint listens,
	// so /api/mcp-info can render accurate client-setup snippets.
	mcpHTTPAddr string
	mcpHTTPPath string
```

Add the builder option after `WithTrustXFF` (~line 143, matching the existing `With*` style):

```go
// WithMCPInfo tells the web server where the remote MCP endpoint is exposed
// (empty addr means remote MCP is disabled).
func (s *Server) WithMCPInfo(addr, path string) *Server {
	s.mcpHTTPAddr = addr
	s.mcpHTTPPath = path
	return s
}
```

Register the route inside the member route group, directly after `r.Get("/api/me/teams", s.handleMyTeams)` (~line 223):

```go
		r.Get("/api/mcp-info", s.handleMCPInfo)
```

- [ ] **Step 6: Wire config in main.go**

In `cmd/server/main.go` (~line 193), add one line to the builder chain (e.g. after `.WithTrustXFF(cfg.TrustXFF).`):

```go
		WithMCPInfo(cfg.MCPHTTPAddr, cfg.MCPHTTPPath).
```

- [ ] **Step 7: Verify full backend build and tests**

Run: `cd /Users/dsandor/Projects/memory && go build ./... && go test ./internal/web/...`
Expected: build succeeds, all tests PASS. **Do not commit.**

---

### Task 2: Frontend lib — shared clipboard util + `getMCPInfo`

**Files:**
- Create: `web/src/lib/clipboard.ts`
- Modify: `web/src/lib/api.ts` (add `MCPInfo` + `getMCPInfo` right after `getMyTeams`, ~line 352)

**Interfaces:**
- Consumes: existing private `apiFetch(url, init)` wrapper in `api.ts` (injects Bearer + X-Team-Id, redirects on 401).
- Produces: `copyToClipboard(text: string): Promise<boolean>` from `@/lib/clipboard`; `interface MCPInfo { http_enabled: boolean; url: string }` and `getMCPInfo(): Promise<MCPInfo>` from `@/lib/api`.

- [ ] **Step 1: Create the clipboard util**

Create `web/src/lib/clipboard.ts`:

```ts
// Copy text to the clipboard. Returns true on success.
// Falls back to a hidden textarea + execCommand for insecure (non-HTTPS)
// contexts where navigator.clipboard is unavailable.
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
```

- [ ] **Step 2: Add MCPInfo to api.ts**

In `web/src/lib/api.ts`, directly after the `getMyTeams` function (~line 352), add:

```ts
export interface MCPInfo {
  http_enabled: boolean;
  url: string;
}

export async function getMCPInfo(): Promise<MCPInfo> {
  const r = await apiFetch('/api/mcp-info');
  if (!r.ok) throw new Error('mcp info failed');
  return r.json();
}
```

- [ ] **Step 3: Verify the web build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: `tsc` + `vite build` succeed with no errors. **Do not commit.**

---

### Task 3: `MCPSetupDialog` component

**Files:**
- Create: `web/src/components/MCPSetupDialog.tsx`

**Interfaces:**
- Consumes: `getMCPInfo`, `MCPInfo`, `APIKey` from `@/lib/api`; `copyToClipboard` from `@/lib/clipboard`.
- Produces: default export `MCPSetupDialog` with props `{ open: boolean; onClose: () => void; keys: APIKey[]; meUserId?: string; onCopied: (ok: boolean) => void }`. Task 4 imports and mounts it.

- [ ] **Step 1: Create the component**

Create `web/src/components/MCPSetupDialog.tsx` exactly as below. Note the `\${TK_AUTH}` escape inside the template literal — the rendered JSON must contain the literal text `${TK_AUTH}` (an env-var reference expanded by Claude Desktop, not by our code); the env-var form sidesteps Claude Desktop's known bug with spaces inside `args` entries.

```tsx
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControl from '@mui/material/FormControl'
import IconButton from '@mui/material/IconButton'
import InputLabel from '@mui/material/InputLabel'
import Link from '@mui/material/Link'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { Check, Copy, X } from 'lucide-react'
import { getMCPInfo, type APIKey, type MCPInfo } from '@/lib/api'
import { copyToClipboard } from '@/lib/clipboard'

interface Props {
  open: boolean
  onClose: () => void
  keys: APIKey[]
  meUserId?: string
  onCopied: (ok: boolean) => void
}

// Dark code block with a copy button in the top-right corner.
function CodeSnippet({ code, onCopied }: { code: string; onCopied: (ok: boolean) => void }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    const ok = await copyToClipboard(code)
    onCopied(ok)
    if (ok) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }
  return (
    <Box sx={{ position: 'relative', mt: 1 }}>
      <Box
        component="pre"
        sx={{
          m: 0,
          p: 1.5,
          pr: 5,
          bgcolor: 'rgba(0,0,0,0.35)',
          border: '1px solid',
          borderColor: 'divider',
          borderRadius: 1,
          fontFamily: 'monospace',
          fontSize: 12.5,
          lineHeight: 1.6,
          overflowX: 'auto',
          whiteSpace: 'pre',
        }}
      >
        {code}
      </Box>
      <Tooltip title={copied ? 'Copied' : 'Copy to clipboard'}>
        <IconButton
          size="small"
          onClick={handleCopy}
          sx={{ position: 'absolute', top: 6, right: 6, color: copied ? 'primary.main' : 'text.secondary' }}
        >
          {copied ? <Check size={15} /> : <Copy size={15} />}
        </IconButton>
      </Tooltip>
    </Box>
  )
}

export default function MCPSetupDialog({ open, onClose, keys, meUserId, onCopied }: Props) {
  const navigate = useNavigate()
  const [tab, setTab] = useState(0)
  const [info, setInfo] = useState<MCPInfo | null>(null)
  const [infoFailed, setInfoFailed] = useState(false)
  const [selectedKeyId, setSelectedKeyId] = useState<string>('')

  // Keys usable in snippets: must have a retained plaintext value.
  // The caller's personal keys come first, then team keys.
  const available = useMemo(() => {
    const usable = keys.filter((k) => k.raw_key)
    const mine = usable.filter((k) => k.key_type === 'user' && k.user_id === meUserId)
    const team = usable.filter((k) => k.key_type === 'team')
    return [...mine, ...team]
  }, [keys, meUserId])

  useEffect(() => {
    if (!open) return
    setInfoFailed(false)
    getMCPInfo()
      .then(setInfo)
      .catch(() => { setInfo(null); setInfoFailed(true) })
  }, [open])

  // Default to the first available key whenever the list changes.
  useEffect(() => {
    if (available.length > 0 && !available.some((k) => k.id === selectedKeyId)) {
      setSelectedKeyId(available[0].id)
    }
  }, [available, selectedKeyId])

  const selected = available.find((k) => k.id === selectedKeyId)
  const serverURL = info?.url || '<server-url>'
  const keyValue = selected?.raw_key || '<your-api-key>'

  const claudeCodeSnippet =
    `claude mcp add --transport http tribal-knowledge \\\n` +
    `  ${serverURL} \\\n` +
    `  --header "Authorization: Bearer ${keyValue}"`

  const desktopSnippet = `{
  "mcpServers": {
    "tribal-knowledge": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "${serverURL}", "--header", "Authorization:\${TK_AUTH}"],
      "env": { "TK_AUTH": "Bearer ${keyValue}" }
    }
  }
}`

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', pr: 1 }}>
        Connect an MCP Client
        <IconButton size="small" onClick={onClose}><X size={16} /></IconButton>
      </DialogTitle>
      <DialogContent>
        {info && !info.http_enabled && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            Remote MCP is not enabled on this server. Set <code>MCP_HTTP_ADDR</code> (e.g. <code>:8081</code>) and
            restart the server, then reopen this dialog.
          </Alert>
        )}
        {infoFailed && (
          <Alert severity="info" sx={{ mb: 2 }}>
            Could not determine the server&apos;s MCP URL — replace <code>&lt;server-url&gt;</code> below with your
            server&apos;s MCP endpoint.
          </Alert>
        )}
        {available.length === 0 && (
          <Alert severity="info" sx={{ mb: 2 }}>
            You have no copyable API keys.{' '}
            <Link component="button" onClick={() => { onClose(); navigate('/api-keys') }} sx={{ verticalAlign: 'baseline' }}>
              Create one on the API Keys page
            </Link>{' '}
            and reopen this dialog, or replace <code>&lt;your-api-key&gt;</code> below.
          </Alert>
        )}

        {available.length > 1 && (
          <FormControl size="small" fullWidth sx={{ mb: 1.5 }}>
            <InputLabel id="mcp-key-select-label">API key</InputLabel>
            <Select
              labelId="mcp-key-select-label"
              label="API key"
              value={selectedKeyId}
              onChange={(e) => setSelectedKeyId(e.target.value)}
            >
              {available.map((k) => (
                <MenuItem key={k.id} value={k.id}>
                  {k.name} ({k.key_type})
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ mb: 1, minHeight: 36 }}>
          <Tab label="Claude Code" sx={{ minHeight: 36, textTransform: 'none' }} />
          <Tab label="Claude Desktop" sx={{ minHeight: 36, textTransform: 'none' }} />
        </Tabs>

        {tab === 0 && (
          <Box>
            <Typography variant="body2" color="text.secondary">
              Run this in your terminal to register the server with Claude Code:
            </Typography>
            <CodeSnippet code={claudeCodeSnippet} onCopied={onCopied} />
          </Box>
        )}
        {tab === 1 && (
          <Box>
            <Typography variant="body2" color="text.secondary">
              Add this to your Claude Desktop config file, then restart Claude Desktop:
            </Typography>
            <Typography variant="caption" component="div" sx={{ mt: 0.5, color: 'text.secondary', fontFamily: 'monospace', fontSize: 11 }}>
              macOS: ~/Library/Application Support/Claude/claude_desktop_config.json
              <br />
              Windows: %APPDATA%\Claude\claude_desktop_config.json
            </Typography>
            <CodeSnippet code={desktopSnippet} onCopied={onCopied} />
            <Typography variant="caption" sx={{ display: 'block', mt: 1, color: 'text.secondary' }}>
              Requires Node.js — the <code>npx mcp-remote</code> bridge connects Claude Desktop to the remote server.
            </Typography>
          </Box>
        )}

        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2 }}>
          <Button size="small" onClick={onClose}>Close</Button>
        </Box>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Verify the web build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: succeeds. (The component isn't mounted yet; this validates types/compilation only.) **Do not commit.**

---

### Task 4: Extend `UserMenu.tsx` — API keys section + MCP Setup entry

**Files:**
- Modify: `web/src/components/UserMenu.tsx`

**Interfaces:**
- Consumes: `listAPIKeys`, `type APIKey` from `@/lib/api`; `copyToClipboard` from `@/lib/clipboard`; `MCPSetupDialog` from `@/components/MCPSetupDialog` (props `{ open, onClose, keys, meUserId, onCopied }`).
- Produces: the finished feature; nothing downstream.

- [ ] **Step 1: Extend imports and state**

In `web/src/components/UserMenu.tsx`:

Replace the lucide import (line 14) with:

```ts
import { Check, Copy, EyeOff, KeyRound, LogOut, Plug, Users } from 'lucide-react'
```

Replace the api import (line 15) with:

```ts
import { getMe, getMyTeams, getActiveTeam, setActiveTeam, listAPIKeys, logout, type APIKey } from '@/lib/api'
```

Add below the existing MUI imports (after line 13):

```ts
import Snackbar from '@mui/material/Snackbar'
import MCPSetupDialog from '@/components/MCPSetupDialog'
import { copyToClipboard } from '@/lib/clipboard'
```

Add state after `const [activeTeam, setActiveTeamState] = useState<string | null>(getActiveTeam())` (line 34):

```ts
  const [keys, setKeys] = useState<APIKey[]>([])
  const [keysLoaded, setKeysLoaded] = useState(false)
  const [mcpOpen, setMcpOpen] = useState(false)
  const [snack, setSnack] = useState<string | null>(null)
```

- [ ] **Step 2: Lazy-load keys on first menu open + copy handler**

Replace `const handleOpen = (e: MouseEvent<HTMLElement>) => setAnchorEl(e.currentTarget)` (line 56) with:

```ts
  const handleOpen = (e: MouseEvent<HTMLElement>) => {
    setAnchorEl(e.currentTarget)
    // Lazy-load keys the first time the menu opens; failures leave the section empty.
    if (!keysLoaded) {
      setKeysLoaded(true)
      listAPIKeys().then(setKeys).catch(() => {})
    }
  }

  const handleCopyKey = async (raw: string) => {
    const ok = await copyToClipboard(raw)
    setSnack(ok ? 'Copied to clipboard' : 'Copy failed')
  }
```

Add the derived key list after `const homeTeamName = ...` (line 74):

```ts
  // Show the caller's personal keys; fall back to team keys when they have none.
  const myKeys = keys.filter((k) => k.key_type === 'user' && k.user_id === me?.user_id)
  const displayKeys = (myKeys.length > 0 ? myKeys : keys.filter((k) => k.key_type === 'team')).slice(0, 3)
```

- [ ] **Step 3: Replace the links section with API-keys section + new links**

Replace the current block from `<Divider />` (line 169) through the "My API Keys" MenuItem (line 179) — i.e. the `{/* 3. Links */}` section — with:

```tsx
        <Divider />

        {/* 3. API keys */}
        <Typography variant="caption" sx={{ px: 2, pt: 1, pb: 0.5, display: 'block', color: 'text.secondary', fontSize: 10, textTransform: 'uppercase', letterSpacing: 0.5 }}>
          API keys
        </Typography>
        {keysLoaded && displayKeys.length === 0 && (
          <MenuItem onClick={() => { handleClose(); navigate('/api-keys') }} dense>
            <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
            <ListItemText primary="No API keys yet — create one" slotProps={{ primary: { style: { fontSize: 13 } } }} />
          </MenuItem>
        )}
        {displayKeys.map((k) => (
          <MenuItem
            key={k.id}
            onClick={() => { if (k.raw_key) handleCopyKey(k.raw_key) }}
            disabled={!k.raw_key}
            dense
          >
            <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
            <ListItemText
              primary={k.name}
              secondary={k.raw_key ? `tk_••••${k.raw_key.slice(-4)}` : 'tk_•••• (not retrievable)'}
              slotProps={{
                primary: { style: { fontSize: 13 } },
                secondary: { style: { fontSize: 11, fontFamily: 'monospace' } },
              }}
            />
            {k.raw_key && <Copy size={13} style={{ opacity: 0.6, flexShrink: 0 }} />}
          </MenuItem>
        ))}
        <MenuItem onClick={() => { handleClose(); navigate('/api-keys') }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><Box sx={{ width: 15 }} /></ListItemIcon>
          <ListItemText primary="Manage keys →" slotProps={{ primary: { style: { fontSize: 12, color: '#94a3b8' } } }} />
        </MenuItem>

        <Divider />

        {/* 4. Links */}
        <MenuItem onClick={() => { handleClose(); navigate('/my-visibility') }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><EyeOff size={15} /></ListItemIcon>
          <ListItemText primary="My Visibility" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
        <MenuItem onClick={() => { handleClose(); setMcpOpen(true) }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><Plug size={15} /></ListItemIcon>
          <ListItemText primary="MCP Setup" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
```

(The Sign Out section that follows keeps its existing `<Divider />` and MenuItem, but renumber its comment to `{/* 5. Sign out */}`.)

- [ ] **Step 4: Mount the dialog and snackbar**

After the closing `</Menu>` tag (line 188), still inside the root `<Box>`, add:

```tsx
      <MCPSetupDialog
        open={mcpOpen}
        onClose={() => setMcpOpen(false)}
        keys={keys}
        meUserId={me?.user_id}
        onCopied={(ok) => setSnack(ok ? 'Copied to clipboard' : 'Copy failed')}
      />
      <Snackbar
        open={snack !== null}
        autoHideDuration={2000}
        onClose={() => setSnack(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        message={snack ?? ''}
      />
```

- [ ] **Step 5: Verify the web build**

Run: `cd /Users/dsandor/Projects/memory/web && npm run build`
Expected: succeeds with no TypeScript errors.

- [ ] **Step 6: Full-stack verification**

Run: `cd /Users/dsandor/Projects/memory && go build ./... && go test ./internal/web/...`
Expected: PASS. **Do not commit — a team member handles all git operations.**
