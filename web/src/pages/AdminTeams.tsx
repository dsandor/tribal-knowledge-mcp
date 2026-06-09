import { useEffect, useState } from 'react';
import { fetchTeams, createTeam, updateTeam, setTeamEnabled } from '../lib/api';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Button from '@mui/material/Button';
import IconButton from '@mui/material/IconButton';
import TextField from '@mui/material/TextField';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Paper from '@mui/material/Paper';
import Table from '@mui/material/Table';
import TableHead from '@mui/material/TableHead';
import TableBody from '@mui/material/TableBody';
import TableRow from '@mui/material/TableRow';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import Chip from '@mui/material/Chip';
import EditIcon from '@mui/icons-material/Edit';
import CheckIcon from '@mui/icons-material/Check';
import CloseIcon from '@mui/icons-material/Close';
import Alert from '@mui/material/Alert';

interface Team { id: string; name: string; domain_patterns: string[]; enabled: boolean; }

interface EditState { name: string; patterns: string; }

export default function AdminTeams() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [loading, setLoading] = useState(true);
  const [newName, setNewName] = useState('');
  const [newPatterns, setNewPatterns] = useState('');
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editState, setEditState] = useState<EditState>({ name: '', patterns: '' });
  const [savingId, setSavingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = () => {
    fetchTeams().then((data: unknown) => {
      setTeams(Array.isArray(data) ? data : []);
      setLoading(false);
    }).catch(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    setCreating(true);
    setError(null);
    try {
      await createTeam(newName.trim(), newPatterns.split(',').map(p => p.trim()).filter(Boolean));
      setNewName('');
      setNewPatterns('');
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create team.');
    } finally {
      setCreating(false);
    }
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    await setTeamEnabled(id, !enabled);
    setTeams(prev => prev.map(t => t.id === id ? { ...t, enabled: !enabled } : t));
  };

  const startEdit = (t: Team) => {
    setEditingId(t.id);
    setEditState({
      name: t.name,
      patterns: (t.domain_patterns ?? []).join(', '),
    });
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditState({ name: '', patterns: '' });
  };

  const saveEdit = async (id: string) => {
    if (!editState.name.trim()) return;
    setSavingId(id);
    setError(null);
    try {
      const patterns = editState.patterns.split(',').map(p => p.trim()).filter(Boolean);
      await updateTeam(id, editState.name.trim(), patterns);
      setTeams(prev => prev.map(t =>
        t.id === id ? { ...t, name: editState.name.trim(), domain_patterns: patterns } : t
      ));
      setEditingId(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save team.');
    } finally {
      setSavingId(null);
    }
  };

  if (loading) {
    return (
      <Box sx={{ p: 3, display: 'flex', alignItems: 'center', gap: 2 }}>
        <CircularProgress size={20} />
        <Typography color="text.secondary">Loading...</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: '56rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Teams</Typography>

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>Create Team</Typography>
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
            <TextField
              placeholder="Team name"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleCreate()}
              size="small"
              sx={{ flex: 1, minWidth: 160 }}
            />
            <TextField
              placeholder="Domain patterns (comma-separated regex)"
              value={newPatterns}
              onChange={e => setNewPatterns(e.target.value)}
              size="small"
              sx={{ flex: 2, minWidth: 200 }}
            />
            <Button
              variant="contained"
              onClick={handleCreate}
              disabled={creating || !newName.trim()}
            >
              {creating ? 'Creating...' : 'Create'}
            </Button>
          </Box>
          {error && <Alert severity="error" sx={{ mt: 2 }}>{error}</Alert>}
        </CardContent>
      </Card>

      <TableContainer component={Paper}>
        <Table size="small">
          <TableHead>
            <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: 'rgba(255,255,255,0.04)' } }}>
              <TableCell>Name</TableCell>
              <TableCell>Domain Patterns</TableCell>
              <TableCell align="center">Enabled</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {teams.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} align="center">
                  <Typography color="text.secondary" variant="body2" sx={{ py: 1 }}>No teams yet.</Typography>
                </TableCell>
              </TableRow>
            ) : teams.map(t => {
              const isEditing = editingId === t.id;
              const isSaving = savingId === t.id;
              return (
                <TableRow key={t.id} hover>
                  <TableCell sx={{ fontWeight: 500, minWidth: 140 }}>
                    {isEditing ? (
                      <TextField
                        value={editState.name}
                        onChange={e => setEditState(s => ({ ...s, name: e.target.value }))}
                        size="small"
                        autoFocus
                        sx={{ width: '100%' }}
                      />
                    ) : t.name}
                  </TableCell>
                  <TableCell>
                    {isEditing ? (
                      <TextField
                        value={editState.patterns}
                        onChange={e => setEditState(s => ({ ...s, patterns: e.target.value }))}
                        size="small"
                        placeholder="comma-separated regex"
                        fullWidth
                      />
                    ) : (
                      <Typography variant="body2" color="text.secondary" sx={{ fontFamily: 'monospace', fontSize: 12 }}>
                        {(t.domain_patterns ?? []).join(', ') || '—'}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell align="center">
                    <Chip
                      label={t.enabled ? 'Yes' : 'No'}
                      size="small"
                      color={t.enabled ? 'success' : 'default'}
                    />
                  </TableCell>
                  <TableCell align="right">
                    <Box sx={{ display: 'flex', gap: 0.5, justifyContent: 'flex-end', alignItems: 'center' }}>
                      {isEditing ? (
                        <>
                          <IconButton size="small" onClick={() => saveEdit(t.id)} disabled={isSaving} color="success">
                            <CheckIcon fontSize="small" />
                          </IconButton>
                          <IconButton size="small" onClick={cancelEdit} disabled={isSaving}>
                            <CloseIcon fontSize="small" />
                          </IconButton>
                        </>
                      ) : (
                        <>
                          <IconButton size="small" onClick={() => startEdit(t)} sx={{ color: 'text.secondary' }}>
                            <EditIcon fontSize="small" />
                          </IconButton>
                          <Button
                            variant="text"
                            size="small"
                            onClick={() => handleToggle(t.id, t.enabled)}
                            sx={{ fontSize: 12, color: 'text.secondary', textDecoration: 'underline', minWidth: 0 }}
                          >
                            {t.enabled ? 'Disable' : 'Enable'}
                          </Button>
                        </>
                      )}
                    </Box>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
}
