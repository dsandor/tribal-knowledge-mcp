import { useEffect, useState } from 'react';
import { fetchTeams, createTeam, updateTeam, setTeamEnabled, deleteTeam, type TeamDataCounts } from '../lib/api';
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
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogContentText from '@mui/material/DialogContentText';
import DialogTitle from '@mui/material/DialogTitle';
import FormControl from '@mui/material/FormControl';
import InputLabel from '@mui/material/InputLabel';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import { Trash2 } from 'lucide-react';

interface Team { id: string; name: string; domain_patterns: string[]; enabled: boolean; }

interface EditState { name: string; patterns: string; }

type DeleteMode = 'confirm' | 'migrate';

interface DeleteDialogState {
  open: boolean;
  team: Team | null;
  mode: DeleteMode;
  counts: TeamDataCounts | null;
  migrateTarget: string;
  error: string | null;
  busy: boolean;
  summary: Record<string, number> | null;
}

const EMPTY_DELETE_STATE: DeleteDialogState = {
  open: false,
  team: null,
  mode: 'confirm',
  counts: null,
  migrateTarget: '',
  error: null,
  busy: false,
  summary: null,
};

function formatCounts(counts: TeamDataCounts): string {
  const parts: string[] = [];
  if (counts.users > 0) parts.push(`${counts.users} user${counts.users !== 1 ? 's' : ''}`);
  if (counts.api_keys > 0) parts.push(`${counts.api_keys} API key${counts.api_keys !== 1 ? 's' : ''}`);
  if (counts.entries > 0) parts.push(`${counts.entries} entr${counts.entries !== 1 ? 'ies' : 'y'}`);
  if (counts.clusters > 0) parts.push(`${counts.clusters} cluster${counts.clusters !== 1 ? 's' : ''}`);
  if (counts.agents > 0) parts.push(`${counts.agents} agent${counts.agents !== 1 ? 's' : ''}`);
  if (counts.rules > 0) parts.push(`${counts.rules} rule${counts.rules !== 1 ? 's' : ''}`);
  return parts.join(', ');
}

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
  const [deleteDialog, setDeleteDialog] = useState<DeleteDialogState>(EMPTY_DELETE_STATE);

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

  const openDeleteDialog = (t: Team) => {
    setDeleteDialog({ ...EMPTY_DELETE_STATE, open: true, team: t });
  };

  const closeDeleteDialog = () => {
    setDeleteDialog(EMPTY_DELETE_STATE);
  };

  const handleDeleteConfirm = async () => {
    const { team } = deleteDialog;
    if (!team) return;
    setDeleteDialog(s => ({ ...s, busy: true, error: null }));
    try {
      const result = await deleteTeam(team.id);
      if (result.needsMigration && result.counts) {
        setDeleteDialog(s => ({ ...s, busy: false, mode: 'migrate', counts: result.counts! }));
      } else {
        closeDeleteDialog();
        load();
      }
    } catch (e) {
      setDeleteDialog(s => ({ ...s, busy: false, error: e instanceof Error ? e.message : 'Delete failed.' }));
    }
  };

  const handleMigrateConfirm = async () => {
    const { team, migrateTarget } = deleteDialog;
    if (!team || !migrateTarget) return;
    setDeleteDialog(s => ({ ...s, busy: true, error: null }));
    try {
      const result = await deleteTeam(team.id, migrateTarget);
      if (result.ok) {
        const skipped = result.summary?.agents_skipped ?? 0;
        closeDeleteDialog();
        load();
        if (skipped > 0) {
          // Surface agents_skipped as a page-level note after close
          setError(`${skipped} agent${skipped !== 1 ? 's' : ''} skipped — domain already exists in target.`);
        }
      } else {
        setDeleteDialog(s => ({ ...s, busy: false, error: 'Migration failed.' }));
      }
    } catch (e) {
      setDeleteDialog(s => ({ ...s, busy: false, error: e instanceof Error ? e.message : 'Migration failed.' }));
    }
  };

  const otherTeams = teams.filter(t => t.id !== deleteDialog.team?.id);
  const migrateTargetTeam = otherTeams.find(t => t.id === deleteDialog.migrateTarget);

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
                          <IconButton
                            size="small"
                            onClick={() => openDeleteDialog(t)}
                            sx={{ color: 'error.main' }}
                            title="Delete team"
                          >
                            <Trash2 style={{ width: 16, height: 16 }} />
                          </IconButton>
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

      {/* Delete / Migration dialog */}
      <Dialog open={deleteDialog.open} onClose={deleteDialog.busy ? undefined : closeDeleteDialog} maxWidth="sm" fullWidth>
        {deleteDialog.mode === 'confirm' ? (
          <>
            <DialogTitle>Delete team</DialogTitle>
            <DialogContent>
              <DialogContentText>
                Delete team <strong>{deleteDialog.team?.name}</strong>? This cannot be undone.
              </DialogContentText>
              {deleteDialog.error && (
                <Alert severity="error" sx={{ mt: 2 }}>{deleteDialog.error}</Alert>
              )}
            </DialogContent>
            <DialogActions>
              <Button onClick={closeDeleteDialog} disabled={deleteDialog.busy}>Cancel</Button>
              <Button color="error" variant="contained" onClick={handleDeleteConfirm} disabled={deleteDialog.busy}>
                {deleteDialog.busy ? 'Checking...' : 'Delete'}
              </Button>
            </DialogActions>
          </>
        ) : (
          <>
            <DialogTitle>Migrate data before deleting</DialogTitle>
            <DialogContent>
              <DialogContentText sx={{ mb: 2 }}>
                <strong>{deleteDialog.team?.name}</strong> contains data that must be migrated before deletion:{' '}
                {deleteDialog.counts ? formatCounts(deleteDialog.counts) : ''}.
              </DialogContentText>
              <FormControl fullWidth size="small">
                <InputLabel>Migrate data to</InputLabel>
                <Select
                  label="Migrate data to"
                  value={deleteDialog.migrateTarget}
                  onChange={e => setDeleteDialog(s => ({ ...s, migrateTarget: e.target.value }))}
                  disabled={deleteDialog.busy}
                >
                  {otherTeams.map(ot => (
                    <MenuItem key={ot.id} value={ot.id}>{ot.name}</MenuItem>
                  ))}
                </Select>
              </FormControl>
              {deleteDialog.error && (
                <Alert severity="error" sx={{ mt: 2 }}>{deleteDialog.error}</Alert>
              )}
            </DialogContent>
            <DialogActions>
              <Button onClick={closeDeleteDialog} disabled={deleteDialog.busy}>Cancel</Button>
              <Button
                color="error"
                variant="contained"
                onClick={handleMigrateConfirm}
                disabled={deleteDialog.busy || !deleteDialog.migrateTarget}
              >
                {deleteDialog.busy
                  ? 'Migrating...'
                  : `Move data to ${migrateTargetTeam?.name ?? '…'} and delete`}
              </Button>
            </DialogActions>
          </>
        )}
      </Dialog>
    </Box>
  );
}
