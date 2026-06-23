import { useEffect, useState } from 'react';
import {
  fetchSettings,
  putSettings,
  fetchModelOptions,
  importEnvSettings,
  type AISettings,
  type AIFieldValue,
  type AITouchpoint,
  type ModelOption,
  type ModelOptions,
} from '../lib/api';
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
import Chip from '@mui/material/Chip';
import Autocomplete from '@mui/material/Autocomplete';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import InputLabel from '@mui/material/InputLabel';
import FormControl from '@mui/material/FormControl';
import FormHelperText from '@mui/material/FormHelperText';
import BackupRestore from '../components/BackupRestore';

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
  llm_provider?: string;
  ollama_llm_model?: string;
  embedding_max_tokens?: number;
  chunk_overlap_tokens?: number;
  max_chunks?: number;
  ai_touchpoints?: Record<string, AITouchpoint>;
  ai?: AISettings;
}

// Renders a small "Effective:" line with a source chip.
function EffectiveLine({
  field,
  isKey = false,
}: {
  field: AIFieldValue;
  isKey?: boolean;
}) {
  const displayValue = isKey
    ? field.effective === '' ? 'not set' : 'stored'
    : field.effective === '' ? 'not set' : field.effective;

  let chipColor: 'success' | 'info' | 'default';
  let chipLabel: string;
  if (field.source === 'saved') {
    chipColor = 'success';
    chipLabel = 'saved';
  } else if (field.source === 'env') {
    chipColor = 'info';
    chipLabel = 'from env';
  } else {
    chipColor = 'default';
    chipLabel = 'not set';
  }

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: -1.5 }}>
      <Typography variant="caption" color="text.secondary">
        Effective: {displayValue}
      </Typography>
      <Chip
        label={chipLabel}
        color={chipColor}
        size="small"
        sx={{ height: 18, fontSize: '0.65rem' }}
      />
    </Box>
  );
}

// Import-from-env button: only shown when env has a value not yet persisted.
function ImportEnvButton({
  fieldName,
  field,
  onImport,
}: {
  fieldName: string;
  field: AIFieldValue;
  onImport: (fieldName: string) => Promise<void>;
}) {
  const [importing, setImporting] = useState(false);

  // Show when env has a value AND it isn't already the saved source.
  if (!field.env || field.source === 'saved') return null;

  const handleClick = async () => {
    setImporting(true);
    try {
      await onImport(fieldName);
    } finally {
      setImporting(false);
    }
  };

  return (
    <Button
      size="small"
      variant="outlined"
      onClick={handleClick}
      disabled={importing}
      sx={{ alignSelf: 'flex-start', mt: -1 }}
    >
      {importing ? 'Importing…' : 'Import from env'}
    </Button>
  );
}

export default function Settings() {
  const [settings, setSettings] = useState<TeamSettings>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [importSuccess, setImportSuccess] = useState(false);

  // Tracks whether the user has typed a new API key this session.
  const [hasStoredKey, setHasStoredKey] = useState(false);
  const [keyDraft, setKeyDraft] = useState('');

  // AI effective settings (may be absent on older servers).
  const [ai, setAi] = useState<AISettings | null>(null);

  // Model options for dropdowns.
  const [models, setModels] = useState<ModelOptions>({
    anthropic: [],
    ollama: [],
    anthropic_source: 'fallback',
    ollama_source: 'unavailable',
  });

  const loadAll = async () => {
    const [settingsData, modelsData] = await Promise.allSettled([
      fetchSettings() as Promise<TeamSettings>,
      fetchModelOptions(),
    ]);

    if (settingsData.status === 'fulfilled') {
      const d = settingsData.value ?? {};
      if (d.anthropic_api_key === 'stored') {
        setHasStoredKey(true);
        d.anthropic_api_key = '';
      }
      if (d.ai) {
        setAi(d.ai);
        // Also sync the effective key indicator from ai block.
        if (d.ai.anthropic_api_key.effective !== '') {
          setHasStoredKey(true);
        }
      } else {
        // No ai block returned (older server or aiSrc not configured) — clear stale state.
        setAi(null);
      }
      setSettings(d);
    } else {
      // Settings fetch failed — surface the error.
      setLoadError(
        settingsData.reason instanceof Error
          ? settingsData.reason.message
          : 'Failed to load settings.'
      );
    }

    if (modelsData.status === 'fulfilled') {
      setModels(modelsData.value);
    }
    // If models fail, we leave the default empty state — Autocomplete still works as free text.
  };

  useEffect(() => {
    loadAll().finally(() => setLoading(false));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload: TeamSettings = { ...settings };
      delete payload.ai; // ai block is read-only / server-computed
      if (keyDraft.trim()) {
        payload.anthropic_api_key = keyDraft.trim();
      } else {
        delete payload.anthropic_api_key;
      }
      await putSettings(payload);
      if (keyDraft.trim()) {
        setHasStoredKey(true);
        setKeyDraft('');
      }
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
      // Refresh effective values and model list after save.
      await loadAll();
    } finally {
      setSaving(false);
    }
  };

  // Called when user clicks "Import from env" on a specific field.
  const handleImportEnv = async (fieldName: string) => {
    const result = await importEnvSettings([fieldName]);
    const newAi = result.ai;
    setAi(newAi);

    // Also update the editable saved-value inputs to reflect what was imported.
    // ai_touchpoints is not an AIFieldValue and is never passed to handleImportEnv,
    // so cast through AIFieldValue after guarding for undefined.
    const fieldValue = newAi[fieldName as keyof AISettings] as AIFieldValue | undefined;
    if (fieldValue !== undefined) {
      if (fieldName === 'anthropic_api_key') {
        // Key is masked; if effective is now set, treat as stored.
        if (fieldValue.effective !== '') {
          setHasStoredKey(true);
          setKeyDraft('');
        }
      } else {
        const importedValue = fieldValue.saved;
        setSettings(s => ({ ...s, [fieldName]: importedValue }));
      }
    }

    // Re-fetch model options when the key or ollama URL changes — the available
    // model lists may have changed as a result of the import.
    if (fieldName === 'anthropic_api_key' || fieldName === 'ollama_url') {
      fetchModelOptions().then(setModels).catch(() => { /* tolerated */ });
    }

    setImportSuccess(true);
    setTimeout(() => setImportSuccess(false), 2000);
  };

  // Resolve a string-or-ModelOption value from Autocomplete freeSolo.
  const resolveAutocompleteValue = (val: string | ModelOption | null): string => {
    if (!val) return '';
    if (typeof val === 'string') return val;
    return val.id;
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

      {loadError && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {loadError}
        </Alert>
      )}

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

          {/* ── LLM Provider ── */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <FormControl fullWidth>
              <InputLabel id="llm-provider-label">LLM Provider</InputLabel>
              <Select
                labelId="llm-provider-label"
                label="LLM Provider"
                value={settings.llm_provider ?? ''}
                onChange={e => setSettings(s => ({ ...s, llm_provider: e.target.value }))}
              >
                <MenuItem value="">Anthropic (default)</MenuItem>
                <MenuItem value="anthropic">Anthropic</MenuItem>
                <MenuItem value="ollama">Ollama (local)</MenuItem>
              </Select>
              <FormHelperText>
                Selects which LLM backend is used for chat and completion tasks. Defaults to Anthropic when unset.
              </FormHelperText>
            </FormControl>
            {ai && <EffectiveLine field={{ ...ai.llm_provider, effective: ai.llm_provider.effective === '' ? 'anthropic (default)' : ai.llm_provider.effective }} />}
          </Box>

          <Divider />

          {/* ── Anthropic ── */}
          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            Anthropic
          </Typography>

          {/* API Key */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
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
            {ai && (
              <>
                <EffectiveLine field={ai.anthropic_api_key} isKey />
                <ImportEnvButton
                  fieldName="anthropic_api_key"
                  field={ai.anthropic_api_key}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          {/* Anthropic Model */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Autocomplete
              freeSolo
              options={models.anthropic}
              getOptionLabel={(opt) => typeof opt === 'string' ? opt : opt.label}
              value={models.anthropic.find(m => m.id === (settings.anthropic_model ?? '')) ?? (settings.anthropic_model ?? '')}
              onChange={(_e, val) => {
                setSettings(s => ({ ...s, anthropic_model: resolveAutocompleteValue(val as string | ModelOption | null) }));
              }}
              onInputChange={(_e, val, reason) => {
                if (reason === 'input') {
                  setSettings(s => ({ ...s, anthropic_model: val }));
                }
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Anthropic Model"
                  fullWidth
                  placeholder="claude-sonnet-4-6"
                  helperText={
                    models.anthropic_source === 'fallback'
                      ? 'Overrides ANTHROPIC_MODEL env var. (curated list — set API key for live list)'
                      : 'Overrides ANTHROPIC_MODEL env var. E.g. claude-opus-4-8, claude-sonnet-4-6.'
                  }
                />
              )}
            />
            {ai && (
              <>
                <EffectiveLine field={ai.anthropic_model} />
                <ImportEnvButton
                  fieldName="anthropic_model"
                  field={ai.anthropic_model}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          <Divider />

          {/* ── Ollama ── */}
          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            Ollama (local / self-hosted)
          </Typography>

          {/* Ollama URL */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <TextField
              label="Ollama URL"
              fullWidth
              value={settings.ollama_url ?? ''}
              onChange={e => setSettings(s => ({ ...s, ollama_url: e.target.value }))}
              placeholder="http://localhost:11434"
              helperText="Overrides OLLAMA_URL env var. Used for local embeddings when set."
            />
            {ai && (
              <>
                <EffectiveLine field={ai.ollama_url} />
                <ImportEnvButton
                  fieldName="ollama_url"
                  field={ai.ollama_url}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          {/* Ollama Model */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Autocomplete
              freeSolo
              options={models.ollama}
              getOptionLabel={(opt) => typeof opt === 'string' ? opt : opt.label}
              value={models.ollama.find(m => m.id === (settings.ollama_model ?? '')) ?? (settings.ollama_model ?? '')}
              onChange={(_e, val) => {
                setSettings(s => ({ ...s, ollama_model: resolveAutocompleteValue(val as string | ModelOption | null) }));
              }}
              onInputChange={(_e, val, reason) => {
                if (reason === 'input') {
                  setSettings(s => ({ ...s, ollama_model: val }));
                }
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Ollama Model"
                  fullWidth
                  placeholder="nomic-embed-text"
                  helperText={
                    models.ollama_source === 'unavailable'
                      ? 'Ollama not reachable — type a model name'
                      : 'Overrides OLLAMA_MODEL env var.'
                  }
                />
              )}
            />
            {ai && (
              <>
                <EffectiveLine field={ai.ollama_model} />
                <ImportEnvButton
                  fieldName="ollama_model"
                  field={ai.ollama_model}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          {/* Embedding / chunking config */}
          <TextField
            label="Embedding Max Tokens"
            type="number"
            fullWidth
            slotProps={{ htmlInput: { min: 0, step: 1 } }}
            value={settings.embedding_max_tokens ?? 0}
            onChange={e => setSettings(s => ({ ...s, embedding_max_tokens: parseInt(e.target.value) || 0 }))}
            helperText="Max tokens per embedding vector. Larger items are auto-split into chunks. 0 = server default (8192)."
          />

          <TextField
            label="Chunk Overlap Tokens"
            type="number"
            fullWidth
            slotProps={{ htmlInput: { min: 0, step: 1 } }}
            value={settings.chunk_overlap_tokens ?? 0}
            onChange={e => setSettings(s => ({ ...s, chunk_overlap_tokens: parseInt(e.target.value) || 0 }))}
            helperText="Tokens of overlap between adjacent chunks. 0 = server default (128)."
          />

          <TextField
            label="Max Chunks"
            type="number"
            fullWidth
            slotProps={{ htmlInput: { min: 0, step: 1 } }}
            value={settings.max_chunks ?? 0}
            onChange={e => setSettings(s => ({ ...s, max_chunks: parseInt(e.target.value) || 0 }))}
            helperText="Safety cap on chunks per item. 0 = unlimited/server default (64)."
          />

          {/* Ollama LLM Model */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Autocomplete
              freeSolo
              options={models.ollama}
              getOptionLabel={(opt) => typeof opt === 'string' ? opt : opt.label}
              value={models.ollama.find(m => m.id === (settings.ollama_llm_model ?? '')) ?? (settings.ollama_llm_model ?? '')}
              onChange={(_e, val) => {
                setSettings(s => ({ ...s, ollama_llm_model: resolveAutocompleteValue(val as string | ModelOption | null) }));
              }}
              onInputChange={(_e, val, reason) => {
                if (reason === 'input') {
                  setSettings(s => ({ ...s, ollama_llm_model: val }));
                }
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Ollama Chat Model"
                  fullWidth
                  placeholder="llama3"
                  helperText={
                    models.ollama_source === 'unavailable'
                      ? 'Used when provider is Ollama; separate from the embedding model. (Ollama not reachable — type a model name)'
                      : 'Used when provider is Ollama; separate from the embedding model.'
                  }
                />
              )}
            />
            {ai && (
              <>
                <EffectiveLine field={ai.ollama_llm_model} />
                <ImportEnvButton
                  fieldName="ollama_llm_model"
                  field={ai.ollama_llm_model}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          <Divider />

          {/* ── Agent ── */}
          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            Agent Pipeline
          </Typography>

          {/* Agent Model */}
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
            <Autocomplete
              freeSolo
              options={models.anthropic}
              getOptionLabel={(opt) => typeof opt === 'string' ? opt : opt.label}
              value={models.anthropic.find(m => m.id === (settings.agent_model ?? '')) ?? (settings.agent_model ?? '')}
              onChange={(_e, val) => {
                setSettings(s => ({ ...s, agent_model: resolveAutocompleteValue(val as string | ModelOption | null) }));
              }}
              onInputChange={(_e, val, reason) => {
                if (reason === 'input') {
                  setSettings(s => ({ ...s, agent_model: val }));
                }
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label="Agent Model"
                  fullWidth
                  helperText="Model used by the agent generation pipeline (Anthropic or Ollama model name)."
                />
              )}
            />
            {ai && (
              <>
                <EffectiveLine field={ai.agent_model} />
                <ImportEnvButton
                  fieldName="agent_model"
                  field={ai.agent_model}
                  onImport={handleImportEnv}
                />
              </>
            )}
          </Box>

          <Divider />

          {/* ── AI Touchpoints ── */}
          <Typography variant="caption" sx={{ fontWeight: 600, letterSpacing: '0.05em', textTransform: 'uppercase', color: 'text.secondary' }}>
            AI Touchpoints
          </Typography>

          <Typography variant="body2" color="text.secondary">
            Override the team default per AI usage. Unset rows inherit the default provider above.
          </Typography>

          {(
            [
              { key: 'analysis',    label: 'Analysis (summaries, scoring, gaps)' },
              { key: 'agents',      label: 'Agent generation & refactor' },
              { key: 'improvement', label: 'Improvement & auto-tagging' },
              { key: 'enrichment',  label: 'Prompt enrichment (enrich_context, prompt_suggest)' },
            ] as const
          ).map(({ key, label }) => {
            const tp = settings.ai_touchpoints?.[key];
            const selectedProvider = tp?.provider ?? '';
            const selectedModel = tp?.model ?? '';

            const modelOptions: ModelOption[] =
              selectedProvider === 'anthropic' ? models.anthropic :
              selectedProvider === 'ollama'    ? models.ollama :
              [];

            const handleProviderChange = (newProvider: string) => {
              setSettings(s => {
                const current = { ...(s.ai_touchpoints ?? {}) };
                if (newProvider === '') {
                  delete current[key];
                } else {
                  current[key] = { provider: newProvider, model: '' };
                }
                return { ...s, ai_touchpoints: current };
              });
            };

            const handleModelChange = (newModel: string) => {
              setSettings(s => {
                const current = { ...(s.ai_touchpoints ?? {}) };
                current[key] = { provider: selectedProvider, model: newModel };
                return { ...s, ai_touchpoints: current };
              });
            };

            return (
              <Box key={key} sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>{label}</Typography>
                <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-start' }}>
                  <FormControl sx={{ minWidth: 180 }}>
                    <InputLabel id={`tp-provider-label-${key}`}>Provider</InputLabel>
                    <Select
                      labelId={`tp-provider-label-${key}`}
                      label="Provider"
                      value={selectedProvider}
                      onChange={e => handleProviderChange(e.target.value)}
                    >
                      <MenuItem value="">Default</MenuItem>
                      <MenuItem value="anthropic">Anthropic</MenuItem>
                      <MenuItem value="ollama">Ollama (local)</MenuItem>
                    </Select>
                    <FormHelperText>Inherits team default when unset.</FormHelperText>
                  </FormControl>

                  <Box sx={{ flex: 1 }}>
                    <Autocomplete
                      freeSolo
                      disabled={selectedProvider === ''}
                      options={modelOptions}
                      getOptionLabel={(opt) => typeof opt === 'string' ? opt : opt.label}
                      value={
                        modelOptions.find(m => m.id === selectedModel) ?? selectedModel
                      }
                      onChange={(_e, val) => {
                        handleModelChange(resolveAutocompleteValue(val as string | ModelOption | null));
                      }}
                      onInputChange={(_e, val, reason) => {
                        if (reason === 'input') {
                          handleModelChange(val);
                        }
                      }}
                      renderInput={(params) => (
                        <TextField
                          {...params}
                          label="Model"
                          fullWidth
                          helperText={
                            selectedProvider === ''
                              ? 'Select a provider to choose a model.'
                              : selectedProvider === 'ollama' && models.ollama_source === 'unavailable'
                                ? 'Ollama not reachable — type a model name.'
                                : 'Leave blank to use the provider default for this touchpoint.'
                          }
                        />
                      )}
                    />
                  </Box>
                </Box>
              </Box>
            );
          })}
        </CardContent>
      </Card>

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button variant="contained" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Settings'}
        </Button>
      </Box>

      {/* Backup & Restore (superadmin only — enforced server-side) */}
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, mt: 4 }}>
        Server Administration
      </Typography>
      <BackupRestore />

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

      <Snackbar
        open={importSuccess}
        autoHideDuration={2000}
        onClose={() => setImportSuccess(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity="info" onClose={() => setImportSuccess(false)} sx={{ width: '100%' }}>
          Imported from environment variable.
        </Alert>
      </Snackbar>
    </Box>
  );
}
