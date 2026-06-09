# API Keys Management UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a standalone `/api-keys` page with two sections (Team Keys, User Keys) that lets admins create and revoke API keys, revealing the raw key inline once after creation.

**Architecture:** New `APIKeys.tsx` page component consuming three new api.ts helpers (`listAPIKeys`, `revokeAPIKey`, `listUsers`). Wired into App.tsx routing and Layout.tsx nav. No backend changes required — all REST endpoints already exist.

**Tech Stack:** React 18, TypeScript, MUI v6 (Box, Table, Button, TextField, Select, Chip, Alert, CircularProgress, Snackbar), lucide-react icons, existing `apiFetch` wrapper in `web/src/lib/api.ts`.

---

## File Map

| File | Change |
|------|--------|
| `web/src/lib/api.ts` | Add `listAPIKeys()`, `revokeAPIKey(id)`, `listUsers()`, extend `createAPIKey` to accept optional `userId` |
| `web/src/pages/APIKeys.tsx` | New page — Team Keys + User Keys sections, inline create form, key reveal row |
| `web/src/App.tsx` | Add `import APIKeys` + `<Route path="api-keys" element={<APIKeys />} />` |
| `web/src/components/Layout.tsx` | Add `{ to: '/api-keys', label: 'API Keys', Icon: KeyRound }` to nav array, import `KeyRound` from lucide-react |

---

### Task 1: Add API client helpers

**Files:**
- Modify: `web/src/lib/api.ts` (around line 374 in the `--- API Keys ---` section)

- [ ] **Step 1: Add interfaces and three new functions**

Find the `// --- API Keys ---` comment block (around line 373) and replace the existing `createAPIKey` function with this expanded block:

```typescript
// --- API Keys ---
export interface APIKey {
  id: string;
  team_id: string;
  user_id: string;
  key_type: string;   // "team" | "user"
  name: string;
  role: string;
  created_at: string;
}

export async function listAPIKeys(): Promise<APIKey[]> {
  const r = await apiFetch('/api/api-keys');
  if (!r.ok) throw new Error('list api keys failed');
  return r.json();
}

export async function createAPIKey(
  name: string,
  role: string,
  keyType: string,
  userId?: string,
): Promise<{ id: string; raw_key: string; name: string; role: string; key_type: string; created_at: string }> {
  const r = await apiFetch('/api/api-keys', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, role, key_type: keyType, ...(userId ? { user_id: userId } : {}) }),
  });
  if (!r.ok) throw new Error('create api key failed');
  return r.json();
}

export async function revokeAPIKey(id: string): Promise<void> {
  const r = await apiFetch(`/api/api-keys/${id}`, { method: 'DELETE' });
  if (!r.ok) throw new Error('revoke api key failed');
}

export interface TeamUser {
  id: string;
  team_id: string;
  email: string;
  name: string;
  role: string;
}

export async function listUsers(): Promise<TeamUser[]> {
  const r = await apiFetch('/api/users');
  if (!r.ok) throw new Error('list users failed');
  return r.json();
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/dsandor/Projects/memory/web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors related to api.ts.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat: add listAPIKeys, revokeAPIKey, listUsers API client helpers"
```

---

### Task 2: Build the APIKeys page

**Files:**
- Create: `web/src/pages/APIKeys.tsx`

- [ ] **Step 1: Create the file with imports and types**

```typescript
import { useEffect, useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import Table from '@mui/material/Table';
import TableHead from '@mui/material/TableHead';
import TableBody from '@mui/material/TableBody';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import Paper from '@mui/material/Paper';
import Chip from '@mui/material/Chip';
import Alert from '@mui/material/Alert';
import Snackbar from '@mui/material/Snackbar';
import IconButton from '@mui/material/IconButton';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import {
  listAPIKeys, createAPIKey, revokeAPIKey, listUsers,
  type APIKey, type TeamUser,
} from '../lib/api';

// A newly created key before the user dismisses the reveal row.
interface NewKey extends APIKey {
  raw_key: string;
}
```

- [ ] **Step 2: Add the InlineCreateForm sub-component**

Append to `APIKeys.tsx`:

```typescript
interface InlineCreateFormProps {
  keyType: 'team' | 'user';
  users: TeamUser[];
  onCreated: (key: NewKey) => void;
  onCancel: () => void;
}

function InlineCreateForm({ keyType, users, onCreated, onCancel }: InlineCreateFormProps) {
  const [name, setName] = useState('');
  const [role, setRole] = useState('member');
  const [userId, setUserId] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleCreate = async () => {
    if (!name.trim()) return;
    if (keyType === 'user' && !userId) { setError('Select a user.'); return; }
    setSaving(true);
    setError(null);
    try {
      const result = await createAPIKey(name.trim(), role, keyType, keyType === 'user' ? userId : undefined);
      onCreated({
        id: result.id,
        team_id: '',
        user_id: userId,
        key_type: keyType,
        name: result.name,
        role: result.role,
        created_at: result.created_at,
        raw_key: result.raw_key,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create key.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Box sx={{ p: 2, border: '1px solid', borderColor: 'primary.dark', borderRadius: 1, bgcolor: 'rgba(30,58,95,0.15)', mb: 2 }}>
      <Typography variant="subtitle2" sx={{ fontWeight: 600, mb: 1.5 }}>
        New {keyType === 'team' ? 'Team' : 'User'} Key
      </Typography>
      <Box sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <TextField
          label="Name"
          size="small"
          value={name}
          onChange={e => setName(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleCreate()}
          sx={{ flex: 1, minWidth: 160 }}
          autoFocus
        />
        <FormControl size="small" sx={{ minWidth: 130 }}>
          <InputLabel>Role</InputLabel>
          <Select label="Role" value={role} onChange={e => setRole(e.target.value)}>
            <MenuItem value="member">member</MenuItem>
            <MenuItem value="curator">curator</MenuItem>
            <MenuItem value="admin">admin</MenuItem>
          </Select>
        </FormControl>
        {keyType === 'user' && (
          <FormControl size="small" sx={{ minWidth: 180 }}>
            <InputLabel>User</InputLabel>
            <Select label="User" value={userId} onChange={e => setUserId(e.target.value)}>
              {users.map(u => (
                <MenuItem key={u.id} value={u.id}>{u.email || u.name || u.id}</MenuItem>
              ))}
            </Select>
          </FormControl>
        )}
        <Button variant="contained" size="small" onClick={handleCreate} disabled={saving || !name.trim()}>
          {saving ? 'Creating…' : 'Create'}
        </Button>
        <Button variant="text" size="small" onClick={onCancel} disabled={saving} sx={{ color: 'text.secondary' }}>
          Cancel
        </Button>
      </Box>
      {error && <Alert severity="error" sx={{ mt: 1 }}>{error}</Alert>}
    </Box>
  );
}
```

- [ ] **Step 3: Add the KeysSection sub-component**

Append to `APIKeys.tsx`:

```typescript
interface KeysSectionProps {
  title: string;
  subtitle: string;
  keyType: 'team' | 'user';
  keys: APIKey[];
  newKey: NewKey | null;
  users: TeamUser[];
  onRevokeDone: (id: string) => void;
  onCreated: (key: NewKey) => void;
  onDismissReveal: () => void;
}

function KeysSection({
  title, subtitle, keyType, keys, newKey, users,
  onRevokeDone, onCreated, onDismissReveal,
}: KeysSectionProps) {
  const [showForm, setShowForm] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const handleRevoke = async (id: string) => {
    setRevoking(id);
    try {
      await revokeAPIKey(id);
      onRevokeDone(id);
    } finally {
      setRevoking(null);
    }
  };

  const handleCopy = (raw: string) => {
    navigator.clipboard.writeText(raw);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCreated = (key: NewKey) => {
    setShowForm(false);
    onCreated(key);
  };

  const cols = keyType === 'user' ? 5 : 4; // extra User column for user keys

  return (
    <Box sx={{ mb: 4 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
        <Box>
          <Typography variant="h6" component="span" sx={{ fontWeight: 600 }}>{title}</Typography>
          <Typography variant="body2" color="text.secondary" component="span" sx={{ ml: 1 }}>{subtitle}</Typography>
        </Box>
        <Button variant="outlined" size="small" onClick={() => setShowForm(v => !v)}>
          + New {keyType === 'team' ? 'Team' : 'User'} Key
        </Button>
      </Box>

      {showForm && (
        <InlineCreateForm
          keyType={keyType}
          users={users}
          onCreated={handleCreated}
          onCancel={() => setShowForm(false)}
        />
      )}

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' } }}>
              <TableCell>Name</TableCell>
              <TableCell>Key</TableCell>
              {keyType === 'user' && <TableCell>User</TableCell>}
              <TableCell>Role</TableCell>
              <TableCell>Created</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {/* Reveal row for newly created key */}
            {newKey && (
              <TableRow sx={{ bgcolor: 'rgba(20,83,45,0.25)', '& td': { borderColor: 'success.dark' } }}>
                <TableCell sx={{ fontWeight: 600 }}>{newKey.name} ✨</TableCell>
                <TableCell>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography sx={{ fontFamily: 'monospace', fontSize: 12, color: 'info.light', wordBreak: 'break-all' }}>
                      {newKey.raw_key}
                    </Typography>
                    <IconButton size="small" onClick={() => handleCopy(newKey.raw_key)} title="Copy key">
                      <ContentCopyIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                  </Box>
                  <Typography variant="caption" sx={{ color: 'warning.main', display: 'block', mt: 0.5 }}>
                    ⚠ Copy now — won't be shown again
                  </Typography>
                </TableCell>
                {keyType === 'user' && <TableCell sx={{ color: 'text.secondary', fontSize: 12 }}>{newKey.user_id || '—'}</TableCell>}
                <TableCell><RoleBadge role={newKey.role} /></TableCell>
                <TableCell sx={{ color: 'text.secondary', fontSize: 12 }}>just now</TableCell>
                <TableCell>
                  <Button
                    size="small"
                    variant="contained"
                    color="success"
                    onClick={onDismissReveal}
                    sx={{ fontSize: 11 }}
                  >
                    Got it ✓
                  </Button>
                </TableCell>
              </TableRow>
            )}
            {/* Existing keys */}
            {keys.length === 0 && !newKey ? (
              <TableRow>
                <TableCell colSpan={cols + 1} align="center">
                  <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>
                    No {keyType} keys yet.
                  </Typography>
                </TableCell>
              </TableRow>
            ) : keys.map(k => (
              <TableRow key={k.id} hover>
                <TableCell sx={{ fontWeight: 500 }}>{k.name}</TableCell>
                <TableCell>
                  <Typography sx={{ fontFamily: 'monospace', fontSize: 12, color: 'text.disabled' }}>
                    tk_••••••••••••
                  </Typography>
                </TableCell>
                {keyType === 'user' && (
                  <TableCell sx={{ fontSize: 12, color: 'info.light' }}>
                    {users.find(u => u.id === k.user_id)?.email || k.user_id || '—'}
                  </TableCell>
                )}
                <TableCell><RoleBadge role={k.role} /></TableCell>
                <TableCell sx={{ color: 'text.secondary', fontSize: 12 }}>
                  {k.created_at ? k.created_at.slice(0, 10) : '—'}
                </TableCell>
                <TableCell>
                  <Button
                    size="small"
                    variant="text"
                    color="error"
                    disabled={revoking === k.id}
                    onClick={() => handleRevoke(k.id)}
                    sx={{ fontSize: 11 }}
                  >
                    {revoking === k.id ? '…' : 'Revoke'}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      <Snackbar
        open={copied}
        autoHideDuration={2000}
        onClose={() => setCopied(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        message="Key copied to clipboard"
      />
    </Box>
  );
}
```

- [ ] **Step 4: Add the RoleBadge helper and the main APIKeys component**

Append to `APIKeys.tsx`:

```typescript
const ROLE_COLORS: Record<string, { bg: string; fg: string }> = {
  superadmin: { bg: 'rgba(109,40,217,0.25)', fg: '#c4b5fd' },
  admin:      { bg: 'rgba(30,58,95,0.5)',   fg: '#7dd3fc' },
  curator:    { bg: 'rgba(45,31,0,0.5)',    fg: '#fcd34d' },
  member:     { bg: 'rgba(20,83,45,0.3)',   fg: '#86efac' },
};

function RoleBadge({ role }: { role: string }) {
  const c = ROLE_COLORS[role] ?? { bg: 'rgba(255,255,255,0.1)', fg: '#e2e8f0' };
  return (
    <Chip
      label={role}
      size="small"
      sx={{ bgcolor: c.bg, color: c.fg, fontWeight: 500, fontSize: 11, height: 20 }}
    />
  );
}

export default function APIKeys() {
  const [allKeys, setAllKeys] = useState<APIKey[]>([]);
  const [users, setUsers] = useState<TeamUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [newTeamKey, setNewTeamKey] = useState<NewKey | null>(null);
  const [newUserKey, setNewUserKey] = useState<NewKey | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    Promise.all([listAPIKeys(), listUsers()])
      .then(([keys, us]) => {
        setAllKeys(Array.isArray(keys) ? keys : []);
        setUsers(Array.isArray(us) ? us : []);
      })
      .catch(() => setError('Failed to load API keys.'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const teamKeys = allKeys.filter(k => k.key_type === 'team');
  const userKeys = allKeys.filter(k => k.key_type === 'user');

  const handleRevoke = (id: string) => setAllKeys(prev => prev.filter(k => k.id !== id));

  const handleTeamCreated = (key: NewKey) => {
    setNewTeamKey(key);
    // Add masked version to the list (reveal row handles the raw display)
    setAllKeys(prev => [{ ...key, key_type: 'team' }, ...prev]);
  };

  const handleUserCreated = (key: NewKey) => {
    setNewUserKey(key);
    setAllKeys(prev => [{ ...key, key_type: 'user' }, ...prev]);
  };

  if (loading) {
    return (
      <Box sx={{ p: 3, display: 'flex', alignItems: 'center', gap: 2 }}>
        <CircularProgress size={20} />
        <Typography color="text.secondary">Loading API keys…</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: '64rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>API Keys</Typography>
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      <KeysSection
        title="Team Keys"
        subtitle="Shared credentials not tied to a specific user"
        keyType="team"
        keys={teamKeys.filter(k => !newTeamKey || k.id !== newTeamKey.id)}
        newKey={newTeamKey}
        users={users}
        onRevokeDone={handleRevoke}
        onCreated={handleTeamCreated}
        onDismissReveal={() => setNewTeamKey(null)}
      />

      <KeysSection
        title="User Keys"
        subtitle="Personal keys tied to a team member"
        keyType="user"
        keys={userKeys.filter(k => !newUserKey || k.id !== newUserKey.id)}
        newKey={newUserKey}
        users={users}
        onRevokeDone={handleRevoke}
        onCreated={handleUserCreated}
        onDismissReveal={() => setNewUserKey(null)}
      />
    </Box>
  );
}
```

- [ ] **Step 5: Verify TypeScript compiles**

```bash
cd /Users/dsandor/Projects/memory/web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/APIKeys.tsx
git commit -m "feat: add APIKeys page with team/user sections and inline key reveal"
```

---

### Task 3: Wire route and nav

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Layout.tsx`

- [ ] **Step 1: Add route to App.tsx**

In `web/src/App.tsx`, add the import after the existing page imports:

```typescript
import APIKeys from './pages/APIKeys'
```

Then add the route inside the Layout route group (after the `pipeline` route):

```typescript
<Route path="api-keys" element={<APIKeys />} />
```

- [ ] **Step 2: Add nav entry to Layout.tsx**

In `web/src/components/Layout.tsx`, add `KeyRound` to the lucide-react import:

```typescript
import {
  LayoutDashboard, BookOpen, Upload, Network, Database,
  Bot, BarChart2, Clock, Settings, Users, ShieldCheck, LogOut, Activity, KeyRound,
} from 'lucide-react'
```

Then add the nav entry to the `nav` array after the `settings` entry:

```typescript
  { to: '/settings', label: 'Settings', Icon: Settings },
  { to: '/api-keys', label: 'API Keys', Icon: KeyRound },
  { to: '/admin/teams', label: 'Teams', Icon: Users },
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd /Users/dsandor/Projects/memory/web && npx tsc --noEmit 2>&1 | head -30
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/components/Layout.tsx
git commit -m "feat: wire /api-keys route and nav entry"
```

---

### Task 4: Build verification

**Files:** None — verification only.

- [ ] **Step 1: Run the Vite production build**

```bash
cd /Users/dsandor/Projects/memory/web && npm run build 2>&1 | tail -20
```

Expected: `✓ built in` with no TypeScript or import errors.

- [ ] **Step 2: Verify Go still builds**

```bash
cd /Users/dsandor/Projects/memory && go build ./... 2>&1 | grep -v deprecated
```

Expected: only the sqlite-vec CGO deprecation warnings, no errors.

- [ ] **Step 3: Smoke-test the running server**

Start the server with `DEV_BYPASS_AUTH=true`:

```bash
DEV_BYPASS_AUTH=true HTTP_ADDR=:8080 go run ./cmd/server &
```

Then:
1. Open `http://localhost:8080` — sidebar should show "API Keys" between Settings and Teams
2. Navigate to `/api-keys` — page loads with two empty sections
3. Click "+ New Team Key", fill in name `test-key`, role `member`, click Create
4. Confirm the reveal row appears with the full key and a Copy button
5. Click "Got it ✓" — row collapses to masked `tk_••••••••••••`
6. Click Revoke on the masked row — row disappears

Kill the server: `kill %1`

- [ ] **Step 4: Final commit if any fixes were needed**

```bash
git add -p
git commit -m "fix: api-keys page smoke-test corrections"
```
