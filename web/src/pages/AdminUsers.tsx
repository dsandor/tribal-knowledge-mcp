import { useEffect, useState } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';
import Table from '@mui/material/Table';
import TableHead from '@mui/material/TableHead';
import TableBody from '@mui/material/TableBody';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import Paper from '@mui/material/Paper';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import Chip from '@mui/material/Chip';
import { listAllUsers, fetchTeams, assignUserTeam, type AdminUser } from '../lib/api';

interface Team { id: string; name: string; }

export default function AdminUsers() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<Record<string, boolean>>({});
  const [saveErrors, setSaveErrors] = useState<Record<string, string>>({});

  useEffect(() => {
    Promise.all([listAllUsers(), fetchTeams()])
      .then(([us, ts]) => {
        setUsers(Array.isArray(us) ? us : []);
        setTeams(Array.isArray(ts) ? ts : []);
      })
      .catch(e => setError(e instanceof Error ? e.message : 'Failed to load'))
      .finally(() => setLoading(false));
  }, []);

  const handleTeamChange = async (user: AdminUser, newTeamId: string) => {
    setSaving(s => ({ ...s, [user.id]: true }));
    setSaveErrors(e => ({ ...e, [user.id]: '' }));
    try {
      await assignUserTeam(user.id, newTeamId, user.role);
      setUsers(us => us.map(u =>
        u.id === user.id ? { ...u, team_id: newTeamId, manually_assigned: true } : u
      ));
    } catch (e) {
      setSaveErrors(errs => ({ ...errs, [user.id]: e instanceof Error ? e.message : 'Failed' }));
    } finally {
      setSaving(s => ({ ...s, [user.id]: false }));
    }
  };

  if (loading) {
    return (
      <Box sx={{ p: 3, display: 'flex', gap: 2, alignItems: 'center' }}>
        <CircularProgress size={20} />
        <Typography color="text.secondary">Loading users…</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: '64rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 0.5 }}>All Users</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Manually assign users to teams. This overrides domain-pattern auto-assignment and is useful when all users share the same email domain.
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' } }}>
              <TableCell>Email</TableCell>
              <TableCell>Name</TableCell>
              <TableCell>Role</TableCell>
              <TableCell>Team</TableCell>
              <TableCell>Pinned</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {users.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} align="center">
                  <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>No users found.</Typography>
                </TableCell>
              </TableRow>
            ) : users.map(u => (
              <TableRow key={u.id} hover>
                <TableCell sx={{ fontWeight: 500 }}>{u.email || '—'}</TableCell>
                <TableCell sx={{ color: 'text.secondary', fontSize: 13 }}>{u.name || '—'}</TableCell>
                <TableCell>
                  <Chip label={u.role} size="small" sx={{ fontSize: 11 }} />
                </TableCell>
                <TableCell sx={{ minWidth: 180 }}>
                  <Select
                    size="small"
                    value={u.team_id}
                    onChange={e => handleTeamChange(u, e.target.value)}
                    disabled={saving[u.id] || teams.length === 0}
                    displayEmpty
                    sx={{ fontSize: 13, minWidth: 160 }}
                  >
                    {u.team_id === '' && (
                      <MenuItem value="" disabled><em>— unassigned —</em></MenuItem>
                    )}
                    {teams.map(t => (
                      <MenuItem key={t.id} value={t.id}>{t.name}</MenuItem>
                    ))}
                  </Select>
                  {saveErrors[u.id] && (
                    <Typography variant="caption" color="error" sx={{ display: 'block', mt: 0.5 }}>
                      {saveErrors[u.id]}
                    </Typography>
                  )}
                </TableCell>
                <TableCell>
                  {u.manually_assigned && (
                    <Chip label="pinned" size="small" color="info" sx={{ fontSize: 10, height: 18 }} />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
}
