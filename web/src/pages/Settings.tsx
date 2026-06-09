import { useEffect, useState } from 'react';
import { fetchSettings, putSettings } from '../lib/api';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Alert from '@mui/material/Alert';
import Snackbar from '@mui/material/Snackbar';
import Divider from '@mui/material/Divider';

interface TeamSettings {
  team_id?: string;
  domains?: string[];
  cluster_threshold?: number;
  pipeline_min_entries?: number;
  agent_model?: string;
  anthropic_api_key?: string;
  anthropic_model?: string;
  ollama_url?: string;
  ollama_model?: string;
}

export default function Settings() {
  const [settings, setSettings] = useState<TeamSettings>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  // Tracks whether the user has typed a new API key this session.
  // We show a placeholder when one is already stored but not expose the value.
  const [hasStoredKey, setHasStoredKey] = useState(false);
  const [keyDraft, setKeyDraft] = useState('');

  useEffect(() => {
    fetchSettings().then((data: TeamSettings) => {
      const d = data ?? {};
      // Detect a stored key without exposing it in the input.
      if (d.anthropic_api_key === 'stored') {
        setHasStoredKey(true);
        d.anthropic_api_key = '';
      }
      setSettings(d);
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload: TeamSettings = { ...settings };
      // Only send the key if the user typed a new one.
      if (keyDraft.trim()) {
        payload.anthropic_api_key = keyDraft.trim();
      } else {
        // Send empty string to leave the existing key unchanged on the server.
        // The server should treat '' as "no change" — or we omit the field.
        delete payload.anthropic_api_key;
      }
      await putSettings(payload);
      if (keyDraft.trim()) {
        setHasStoredKey(true);
        setKeyDraft('');
      }
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } finally {
      setSaving(false);
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
    <Box sx={{ p: 3, maxWidth: '40rem' }}>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Settings</Typography>

      {/* Pipeline settings */}
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5 }}>
        Pipeline
      </Typography>
      <Card sx={{ mb: 3 }}>
        <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          <TextField
            label="Domains (comma-separated)"
            fullWidth
            value={(settings.domains ?? []).join(', ')}
            onChange={e => setSettings(s => ({
              ...s,
              domains: e.target.value.split(',').map(d => d.trim()).filter(Boolean),
            }))}
            helperText="Scopes rules and knowledge lookup to these domains."
          />

          <TextField
            label="Cluster Threshold"
            type="number"
            fullWidth
            slotProps={{ htmlInput: { step: 0.01, min: 0.5, max: 1.0 } }}
            value={settings.cluster_threshold ?? 0.85}
            onChange={e => setSettings(s => ({ ...s, cluster_threshold: parseFloat(e.target.value) }))}
            helperText="Cosine similarity cutoff for grouping entries into clusters (0.5–1.0)."
          />

          <TextField
            label="Pipeline Min Entries"
            type="number"
            fullWidth
            slotProps={{ htmlInput: { min: 1 } }}
            value={settings.pipeline_min_entries ?? 10}
            onChange={e => setSettings(s => ({ ...s, pipeline_min_entries: parseInt(e.target.value) }))}
            helperText="Minimum number of knowledge entries required before the pipeline runs."
          />
        </CardContent>
      </Card>

      {/* AI / LLM settings */}
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5 }}>
        AI Configuration
      </Typography>
      <Card sx={{ mb: 3 }}>
        <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          <Typography variant="body2" color="text.secondary">
            These settings override the server-level environment variables for this team.
            Leave a field blank to inherit the server default.
          </Typography>

          <Divider />

          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            Anthropic
          </Typography>

          <TextField
            label="Anthropic API Key"
            type="password"
            fullWidth
            value={keyDraft}
            onChange={e => setKeyDraft(e.target.value)}
            placeholder={hasStoredKey ? '••••••••  (stored — type to replace)' : 'sk-ant-...'}
            helperText={
              hasStoredKey
                ? 'A key is already stored. Type a new one to replace it, or leave blank to keep the existing key.'
                : 'Your Anthropic API key. Stored encrypted. Overrides ANTHROPIC_API_KEY env var.'
            }
          />

          <TextField
            label="Anthropic Model"
            fullWidth
            value={settings.anthropic_model ?? ''}
            onChange={e => setSettings(s => ({ ...s, anthropic_model: e.target.value }))}
            placeholder="claude-sonnet-4-6"
            helperText="Overrides ANTHROPIC_MODEL env var. E.g. claude-opus-4-8, claude-sonnet-4-6."
          />

          <Divider />

          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            Ollama (local / self-hosted)
          </Typography>

          <TextField
            label="Ollama URL"
            fullWidth
            value={settings.ollama_url ?? ''}
            onChange={e => setSettings(s => ({ ...s, ollama_url: e.target.value }))}
            placeholder="http://localhost:11434"
            helperText="Overrides OLLAMA_URL env var. Used for local embeddings when set."
          />

          <TextField
            label="Ollama Model"
            fullWidth
            value={settings.ollama_model ?? ''}
            onChange={e => setSettings(s => ({ ...s, ollama_model: e.target.value }))}
            placeholder="nomic-embed-text"
            helperText="Overrides OLLAMA_MODEL env var."
          />

          <TextField
            label="Agent Model"
            fullWidth
            value={settings.agent_model ?? ''}
            onChange={e => setSettings(s => ({ ...s, agent_model: e.target.value }))}
            helperText="Model used by the agent generation pipeline (Anthropic or Ollama model name)."
          />
        </CardContent>
      </Card>

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button variant="contained" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Settings'}
        </Button>
      </Box>

      <Snackbar
        open={saved}
        autoHideDuration={2000}
        onClose={() => setSaved(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity="success" onClose={() => setSaved(false)} sx={{ width: '100%' }}>
          Settings saved!
        </Alert>
      </Snackbar>
    </Box>
  );
}
