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
import VisibilityIcon from '@mui/icons-material/Visibility';
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff';
import {
  listAPIKeys, createAPIKey, revokeAPIKey, listUsers,
  type APIKey, type TeamUser,
} from '../lib/api';

// A newly created key before the user dismisses the reveal row.
interface NewKey extends APIKey {
  raw_key: string;
}

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
  const [revokeError, setRevokeError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [revealedId, setRevealedId] = useState<string | null>(null);

  const handleRevoke = async (id: string) => {
    setRevoking(id);
    setRevokeError(null);
    try {
      await revokeAPIKey(id);
      onRevokeDone(id);
    } catch (e) {
      setRevokeError(e instanceof Error ? e.message : 'Failed to revoke key.');
    } finally {
      setRevoking(null);
    }
  };

  const handleCopy = async (raw: string) => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(raw);
      } else {
        // Fallback for insecure (non-HTTPS) contexts where navigator.clipboard is unavailable.
        const ta = document.createElement('textarea');
        ta.value = raw;
        ta.style.position = 'fixed';
        ta.style.opacity = '0';
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable */
    }
  };

  const handleCreated = (key: NewKey) => {
    setShowForm(false);
    onCreated(key);
  };

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

      {revokeError && <Alert severity="error" sx={{ mb: 1 }}>{revokeError}</Alert>}

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
                {keyType === 'user' && (
                  <TableCell sx={{ fontSize: 12, color: 'info.light' }}>
                    {users.find(u => u.id === newKey.user_id)?.email || newKey.user_id || '—'}
                  </TableCell>
                )}
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
            {keys.length === 0 && !newKey ? (
              <TableRow>
                <TableCell colSpan={keyType === 'user' ? 6 : 5} align="center">
                  <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>
                    No {keyType} keys yet.
                  </Typography>
                </TableCell>
              </TableRow>
            ) : keys.map(k => (
              <TableRow key={k.id} hover>
                <TableCell sx={{ fontWeight: 500 }}>{k.name}</TableCell>
                <TableCell>
                  {k.raw_key ? (
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                      <Typography sx={{ fontFamily: 'monospace', fontSize: 12, color: revealedId === k.id ? 'info.light' : 'text.disabled', wordBreak: 'break-all' }}>
                        {revealedId === k.id ? k.raw_key : 'tk_••••••••••••'}
                      </Typography>
                      <IconButton
                        size="small"
                        onClick={() => setRevealedId(prev => (prev === k.id ? null : k.id))}
                        title={revealedId === k.id ? 'Hide key' : 'Reveal key'}
                      >
                        {revealedId === k.id
                          ? <VisibilityOffIcon sx={{ fontSize: 14 }} />
                          : <VisibilityIcon sx={{ fontSize: 14 }} />}
                      </IconButton>
                      <IconButton size="small" onClick={() => handleCopy(k.raw_key!)} title="Copy key">
                        <ContentCopyIcon sx={{ fontSize: 14 }} />
                      </IconButton>
                    </Box>
                  ) : (
                    <Typography sx={{ fontFamily: 'monospace', fontSize: 12, color: 'text.disabled' }} title="Created before key copy was supported; cannot be retrieved">
                      tk_••••••••••••
                    </Typography>
                  )}
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
