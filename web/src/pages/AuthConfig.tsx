import { useEffect, useState } from 'react';
import { fetchAuthConfig, putAuthConfig } from '../lib/api';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import Card from '@mui/material/Card';
import CardContent from '@mui/material/CardContent';
import Alert from '@mui/material/Alert';
import Snackbar from '@mui/material/Snackbar';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import InputLabel from '@mui/material/InputLabel';
import FormControl from '@mui/material/FormControl';

interface AuthConfigData {
  provider?: string;
  oidc_issuer?: string;
  oidc_client_id?: string;
  oidc_redirect_url?: string;
}

export default function AuthConfig() {
  const [config, setConfig] = useState<AuthConfigData>({ provider: 'local' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetchAuthConfig().then((data: AuthConfigData) => {
      setConfig(data ?? { provider: 'local' });
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    try {
      await putAuthConfig(config);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed');
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
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Auth Configuration</Typography>

      <Card sx={{ mb: 3 }}>
        <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          <FormControl fullWidth size="small">
            <InputLabel id="provider-label">Provider</InputLabel>
            <Select
              labelId="provider-label"
              label="Provider"
              value={config.provider ?? 'local'}
              onChange={e => setConfig(s => ({ ...s, provider: e.target.value }))}
            >
              <MenuItem value="local">Local (email + password)</MenuItem>
              <MenuItem value="oidc">OIDC</MenuItem>
            </Select>
          </FormControl>

          {config.provider === 'oidc' && (
            <>
              <TextField
                label="OIDC Issuer URL"
                fullWidth
                placeholder="https://accounts.example.com"
                value={config.oidc_issuer ?? ''}
                onChange={e => setConfig(s => ({ ...s, oidc_issuer: e.target.value }))}
              />
              <TextField
                label="OIDC Client ID"
                fullWidth
                value={config.oidc_client_id ?? ''}
                onChange={e => setConfig(s => ({ ...s, oidc_client_id: e.target.value }))}
              />
              <TextField
                label="Redirect URL"
                fullWidth
                placeholder="http://localhost:8080/auth/oidc/callback"
                value={config.oidc_redirect_url ?? ''}
                onChange={e => setConfig(s => ({ ...s, oidc_redirect_url: e.target.value }))}
              />
            </>
          )}
        </CardContent>
      </Card>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>
      )}

      <Button variant="contained" onClick={handleSave} disabled={saving}>
        {saving ? 'Saving...' : 'Save'}
      </Button>

      <Snackbar
        open={saved}
        autoHideDuration={2000}
        onClose={() => setSaved(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity="success" onClose={() => setSaved(false)} sx={{ width: '100%' }}>
          Configuration saved!
        </Alert>
      </Snackbar>
    </Box>
  );
}
