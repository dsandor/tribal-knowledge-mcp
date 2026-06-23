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
import Checkbox from '@mui/material/Checkbox';
import FormGroup from '@mui/material/FormGroup';
import FormControlLabel from '@mui/material/FormControlLabel';
import {
  listUsers,
  addUser,
  setUserRole,
  getMe,
  getMyTeams,
  getUserTeams,
  addMembership,
  removeMembership,
  type TeamUser,
} from '../lib/api';

interface TeamRef {
  id: string;
  name: string;
}

// Membership editor: shows the user's current teams (checked) against the set
// of teams the viewer may assign (superadmin -> all, admin -> own teams).
// Toggling adds/removes a membership and refreshes from the server. The home
// team cannot be removed; the backend rejects it and we surface the message.
function TeamMembershipEditor({ userId, selectableTeams }: { userId: string; selectableTeams: TeamRef[] }) {
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = () => {
    return getUserTeams(userId)
      .then(res => setMemberIds(new Set(res.teams.map(t => t.id))))
      .catch(e => setError(e instanceof Error ? e.message : 'Failed to load teams'));
  };

  useEffect(() => {
    refresh().finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId]);

  const handleToggle = async (teamId: string, checked: boolean) => {
    setError(null);
    setBusy(teamId);
    try {
      if (checked) await addMembership(userId, teamId);
      else await removeMembership(userId, teamId);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to update membership');
    } finally {
      setBusy(null);
    }
  };

  if (loading) return <CircularProgress size={16} />;

  return (
    <Box>
      <FormGroup sx={{ flexDirection: 'row', flexWrap: 'wrap' }}>
        {selectableTeams.map(team => {
          const isMember = memberIds.has(team.id);
          return (
            <FormControlLabel
              key={team.id}
              control={
                <Checkbox
                  size="small"
                  checked={isMember}
                  disabled={busy !== null}
                  onChange={e => handleToggle(team.id, e.target.checked)}
                />
              }
              label={<Typography variant="body2">{team.name}</Typography>}
            />
          );
        })}
      </FormGroup>
      {selectableTeams.length === 0 && (
        <Typography variant="caption" color="text.secondary">No assignable teams.</Typography>
      )}
      {error && (
        <Typography variant="caption" color="error" sx={{ display: 'block', mt: 0.5 }}>
          {error}
        </Typography>
      )}
    </Box>
  );
}

const ROLES = ['member', 'curator', 'admin'] as const;

const ROLE_COLORS: Record<string, 'default' | 'primary' | 'warning' | 'error'> = {
  superadmin: 'error',
  admin: 'warning',
  curator: 'primary',
  member: 'default',
};

function RoleBadge({ role }: { role: string }) {
  return <Chip label={role} size="small" color={ROLE_COLORS[role] ?? 'default'} />;
}

interface AddUserFormProps {
  onAdded: (user: TeamUser) => void;
  onCancel: () => void;
}

function AddUserForm({ onAdded, onCancel }: AddUserFormProps) {
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('member');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleAdd = async () => {
    const trimmed = email.trim();
    if (!trimmed) return;
    setSaving(true);
    setError(null);
    try {
      const result = await addUser(trimmed, role);
      onAdded({ id: result.id, team_id: '', email: trimmed, name: '', role });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to add user');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-start', flexWrap: 'wrap', mb: 2 }}>
      <TextField
        label="Email address"
        size="small"
        value={email}
        onChange={e => setEmail(e.target.value)}
        onKeyDown={e => e.key === 'Enter' && handleAdd()}
        sx={{ minWidth: 260 }}
        autoFocus
      />
      <FormControl size="small" sx={{ minWidth: 130 }}>
        <InputLabel>Role</InputLabel>
        <Select value={role} label="Role" onChange={e => setRole(e.target.value)}>
          {ROLES.map(r => <MenuItem key={r} value={r}>{r}</MenuItem>)}
        </Select>
      </FormControl>
      <Button variant="contained" size="small" onClick={handleAdd} disabled={saving || !email.trim()}>
        {saving ? 'Adding…' : 'Add User'}
      </Button>
      <Button size="small" onClick={onCancel} disabled={saving}>Cancel</Button>
      {error && <Alert severity="error" sx={{ width: '100%', py: 0 }}>{error}</Alert>}
    </Box>
  );
}

export default function Users() {
  const [users, setUsers] = useState<TeamUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [roleErrors, setRoleErrors] = useState<Record<string, string>>({});
  const [myRole, setMyRole] = useState<string>('');
  const [selectableTeams, setSelectableTeams] = useState<TeamRef[]>([]);

  useEffect(() => {
    listUsers()
      .then(setUsers)
      .catch(e => setLoadError(e instanceof Error ? e.message : 'Failed to load users'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    getMe()
      .then(me => setMyRole(me.role))
      .catch(() => setMyRole(''));
  }, []);

  // getMyTeams returns all teams for a superadmin and the admin's own teams
  // for an admin — exactly the set of teams this viewer may assign.
  useEffect(() => {
    getMyTeams()
      .then(res => setSelectableTeams(res.teams))
      .catch(() => setSelectableTeams([]));
  }, []);

  const handleAdded = (user: TeamUser) => {
    setUsers(prev => {
      const existing = prev.findIndex(u => u.id === user.id);
      if (existing >= 0) {
        const next = [...prev];
        next[existing] = user;
        return next;
      }
      return [...prev, user];
    });
    setShowForm(false);
  };

  const handleRoleChange = async (user: TeamUser, newRole: string) => {
    setRoleErrors(prev => ({ ...prev, [user.id]: '' }));
    const prev = user.role;
    setUsers(us => us.map(u => u.id === user.id ? { ...u, role: newRole } : u));
    try {
      await setUserRole(user.id, newRole);
    } catch (e) {
      setUsers(us => us.map(u => u.id === user.id ? { ...u, role: prev } : u));
      setRoleErrors(p => ({ ...p, [user.id]: e instanceof Error ? e.message : 'Failed to update role' }));
    }
  };

  if (loading) return <Box sx={{ display: 'flex', justifyContent: 'center', mt: 8 }}><CircularProgress /></Box>;
  if (loadError) return <Alert severity="error" sx={{ mt: 4 }}>{loadError}</Alert>;

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
        <Box>
          <Typography variant="h5" sx={{ fontWeight: 600 }}>Team Members</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            Manage who has access and what role they hold.
          </Typography>
        </Box>
        {!showForm && (
          <Button variant="contained" size="small" onClick={() => setShowForm(true)}>
            Add User
          </Button>
        )}
      </Box>

      {showForm && (
        <AddUserForm
          onAdded={handleAdded}
          onCancel={() => setShowForm(false)}
        />
      )}

      {users.length === 0 ? (
        <Typography color="text.secondary" sx={{ mt: 4, textAlign: 'center' }}>
          No team members yet. Add one above.
        </Typography>
      ) : (
        <TableContainer component={Paper} variant="outlined">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Email</TableCell>
                <TableCell>Name</TableCell>
                <TableCell>Role</TableCell>
                <TableCell>Teams</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {users.map(user => (
                <TableRow key={user.id} hover>
                  <TableCell>{user.email}</TableCell>
                  <TableCell sx={{ color: 'text.secondary' }}>{user.name || '—'}</TableCell>
                  <TableCell><RoleBadge role={user.role} /></TableCell>
                  <TableCell>
                    <TeamMembershipEditor userId={user.id} selectableTeams={selectableTeams} />
                  </TableCell>
                  <TableCell align="right">
                    <FormControl size="small" sx={{ minWidth: 120 }}>
                      <Select
                        value={user.role}
                        onChange={e => handleRoleChange(user, e.target.value)}
                        displayEmpty
                        variant="outlined"
                        sx={{ fontSize: 13 }}
                      >
                        {ROLES.map(r => <MenuItem key={r} value={r} sx={{ fontSize: 13 }}>{r}</MenuItem>)}
                        {myRole === 'superadmin' && <MenuItem value="superadmin" sx={{ fontSize: 13 }}>superadmin</MenuItem>}
                      </Select>
                    </FormControl>
                    {roleErrors[user.id] && (
                      <Typography variant="caption" color="error" sx={{ display: 'block', mt: 0.5 }}>
                        {roleErrors[user.id]}
                      </Typography>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
