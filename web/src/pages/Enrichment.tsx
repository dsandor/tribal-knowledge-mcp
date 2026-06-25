import { useEffect, useRef, useState } from 'react'
import {
  getEnrichmentPrefs,
  putEnrichmentPrefs,
  previewEnrichment,
  type EnrichmentPrefs,
  type EnrichmentPrefsInput,
  type EnrichmentPreview,
} from '../lib/api'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import CircularProgress from '@mui/material/CircularProgress'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Slider from '@mui/material/Slider'
import Switch from '@mui/material/Switch'
import FormControlLabel from '@mui/material/FormControlLabel'
import Autocomplete from '@mui/material/Autocomplete'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import Alert from '@mui/material/Alert'
import Snackbar from '@mui/material/Snackbar'
import IconButton from '@mui/material/IconButton'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import { X, Pin, Play } from 'lucide-react'

// Editable form state. Scalars are always concrete here; whether they are
// "default" is tracked separately so we can render the "(default)" hint and
// send null on save to revert to the deployment default.
interface FormState {
  min_relevance: number
  max_memories: number
  llm_rewrite: boolean
  min_relevance_default: boolean
  max_memories_default: boolean
  llm_rewrite_default: boolean
  defaults: { min_relevance: number; max_memories: number }
  allow_domains: string[]
  deny_domains: string[]
  allow_tags: string[]
  deny_tags: string[]
  pinned_entries: string[]
}

function prefsToForm(p: EnrichmentPrefs): FormState {
  return {
    min_relevance: p.min_relevance,
    max_memories: p.max_memories,
    llm_rewrite: p.llm_rewrite,
    min_relevance_default: p.min_relevance_default,
    max_memories_default: p.max_memories_default,
    llm_rewrite_default: p.llm_rewrite_default,
    defaults: p.defaults,
    allow_domains: p.allow_domains ?? [],
    deny_domains: p.deny_domains ?? [],
    allow_tags: p.allow_tags ?? [],
    deny_tags: p.deny_tags ?? [],
    pinned_entries: p.pinned_entries ?? [],
  }
}

// Build the PUT/preview override payload from the current form. A scalar that is
// still using the deployment default is sent as null so it stays a default.
function formToInput(f: FormState): EnrichmentPrefsInput {
  return {
    min_relevance: f.min_relevance_default ? null : f.min_relevance,
    max_memories: f.max_memories_default ? null : f.max_memories,
    llm_rewrite: f.llm_rewrite_default ? null : f.llm_rewrite,
    allow_domains: f.allow_domains,
    deny_domains: f.deny_domains,
    allow_tags: f.allow_tags,
    deny_tags: f.deny_tags,
    pinned_entries: f.pinned_entries,
  }
}

const REASON_LABELS: Record<string, string> = {
  below_threshold: 'below threshold',
  denied: 'denied',
  not_in_allowlist: 'not in allow-list',
  over_max: 'over max',
}

function ChipInput({
  label,
  value,
  onChange,
}: {
  label: string
  value: string[]
  onChange: (v: string[]) => void
}) {
  return (
    <Autocomplete<string, true, false, true>
      multiple
      freeSolo
      options={[]}
      value={value}
      onChange={(_e, v) => onChange(v as string[])}
      renderInput={(params) => (
        <TextField {...params} label={label} size="small" placeholder="add…" />
      )}
    />
  )
}

export default function Enrichment() {
  const [form, setForm] = useState<FormState | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  const [prompt, setPrompt] = useState('')
  const [preview, setPreview] = useState<EnrichmentPreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [previewError, setPreviewError] = useState<string | null>(null)

  // Load saved prefs on mount.
  useEffect(() => {
    let cancelled = false
    getEnrichmentPrefs()
      .then((p) => { if (!cancelled) setForm(prefsToForm(p)) })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : 'failed to load prefs') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [])

  // Patch helper that keeps the editor state immutable.
  const patch = (p: Partial<FormState>) => setForm((f) => (f ? { ...f, ...p } : f))

  const runPreview = async (f: FormState, p: string) => {
    setPreviewing(true)
    setPreviewError(null)
    try {
      const res = await previewEnrichment(p, formToInput(f))
      setPreview(res)
    } catch (e) {
      setPreviewError(e instanceof Error ? e.message : 'preview failed')
    } finally {
      setPreviewing(false)
    }
  }

  // Live preview: debounce control/prompt changes ~400ms. Skip empty prompt.
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    if (!form) return
    const trimmed = prompt.trim()
    if (!trimmed) { setPreview(null); return }
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => { void runPreview(form, trimmed) }, 400)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    prompt,
    form?.min_relevance, form?.min_relevance_default,
    form?.max_memories, form?.max_memories_default,
    form?.llm_rewrite, form?.llm_rewrite_default,
    form?.allow_domains, form?.deny_domains, form?.allow_tags, form?.deny_tags,
    form?.pinned_entries,
  ])

  const handleSave = async () => {
    if (!form) return
    setSaving(true)
    setError(null)
    try {
      const refreshed = await putEnrichmentPrefs(formToInput(form))
      setForm(prefsToForm(refreshed))
      setSaved(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  const handleTest = () => {
    if (!form) return
    const trimmed = prompt.trim()
    if (!trimmed) return
    void runPreview(form, trimmed)
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress size={28} />
      </Box>
    )
  }

  if (!form) {
    return (
      <Box>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Enrichment</Typography>
        <Alert severity="error">{error ?? 'Failed to load enrichment preferences.'}</Alert>
      </Box>
    )
  }

  return (
    <Box>
      <Typography variant="h5" sx={{ fontWeight: 700, mb: 0.5 }}>Enrichment</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Tune which memories enrich your prompts, then test the effect live in the playground.
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      <Box sx={{ display: 'flex', gap: 3, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        {/* ── Left: controls ── */}
        <Card sx={{ flex: '1 1 380px', minWidth: 340 }}>
          <CardContent>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 2 }}>Preferences</Typography>

            {/* Min relevance */}
            <Box sx={{ mb: 3 }}>
              <Typography variant="body2" sx={{ mb: 0.5 }}>
                Min relevance: {Math.round(form.min_relevance * 100)}%
                {form.min_relevance_default && (
                  <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                    (default)
                  </Typography>
                )}
              </Typography>
              <Slider
                value={Math.round(form.min_relevance * 100)}
                min={0}
                max={100}
                step={1}
                valueLabelDisplay="auto"
                onChange={(_, v) =>
                  patch({ min_relevance: (v as number) / 100, min_relevance_default: false })
                }
              />
            </Box>

            {/* Max memories */}
            <Box sx={{ mb: 3 }}>
              <TextField
                label="Max memories"
                type="number"
                size="small"
                value={form.max_memories}
                onChange={(e) => {
                  const n = parseInt(e.target.value, 10)
                  patch({ max_memories: Number.isFinite(n) ? Math.max(0, n) : 0, max_memories_default: false })
                }}
                helperText={form.max_memories_default ? '(default)' : ' '}
                slotProps={{ htmlInput: { min: 0 } }}
                sx={{ width: 160 }}
              />
            </Box>

            {/* LLM rewrite */}
            <Box sx={{ mb: 2 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={form.llm_rewrite}
                    onChange={(e) => patch({ llm_rewrite: e.target.checked, llm_rewrite_default: false })}
                  />
                }
                label={
                  <span>
                    LLM rewrite
                    {form.llm_rewrite_default && (
                      <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                        (default)
                      </Typography>
                    )}
                  </span>
                }
              />
            </Box>

            <Divider sx={{ my: 2 }} />

            <Stack spacing={2}>
              <ChipInput label="Allow domains" value={form.allow_domains} onChange={(v) => patch({ allow_domains: v })} />
              <ChipInput label="Deny domains" value={form.deny_domains} onChange={(v) => patch({ deny_domains: v })} />
              <ChipInput label="Allow tags" value={form.allow_tags} onChange={(v) => patch({ allow_tags: v })} />
              <ChipInput label="Deny tags" value={form.deny_tags} onChange={(v) => patch({ deny_tags: v })} />
            </Stack>

            <Divider sx={{ my: 2 }} />

            {/* Pinned entries */}
            <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
              Pinned entries ({form.pinned_entries.length})
            </Typography>
            {form.pinned_entries.length === 0 ? (
              <Typography variant="caption" color="text.secondary">
                No pinned entries. Pin entries from the Knowledge browser.
              </Typography>
            ) : (
              <List dense disablePadding>
                {form.pinned_entries.map((id) => (
                  <ListItem
                    key={id}
                    disableGutters
                    secondaryAction={
                      <Tooltip title="Remove pin">
                        <IconButton
                          edge="end"
                          size="small"
                          onClick={() => patch({ pinned_entries: form.pinned_entries.filter((p) => p !== id) })}
                        >
                          <X size={14} />
                        </IconButton>
                      </Tooltip>
                    }
                  >
                    <Pin size={13} style={{ marginRight: 8, opacity: 0.7 }} />
                    <ListItemText
                      primary={id}
                      slotProps={{ primary: { style: { fontFamily: 'monospace', fontSize: 12 } } }}
                    />
                  </ListItem>
                ))}
              </List>
            )}

            <Divider sx={{ my: 2 }} />

            <Button variant="contained" onClick={handleSave} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </CardContent>
        </Card>

        {/* ── Right: playground ── */}
        <Card sx={{ flex: '1 1 420px', minWidth: 360 }}>
          <CardContent>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 2 }}>Playground</Typography>

            <TextField
              label="Prompt"
              multiline
              minRows={3}
              fullWidth
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="Type a prompt to preview what memories would enrich it…"
            />
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 1 }}>
              <Button
                variant="outlined"
                size="small"
                startIcon={<Play size={14} />}
                onClick={handleTest}
                disabled={!prompt.trim() || previewing}
              >
                Test
              </Button>
              {previewing && <CircularProgress size={16} />}
              <Box sx={{ flex: 1 }} />
              <Typography variant="caption" color="text.secondary">live preview (debounced)</Typography>
            </Box>

            {previewError && <Alert severity="error" sx={{ mt: 2 }}>{previewError}</Alert>}

            {preview && (
              <Box sx={{ mt: 3 }}>
                {/* Included */}
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
                  Included ({preview.included.length})
                </Typography>
                {preview.included.length === 0 ? (
                  <Typography variant="caption" color="text.secondary">No memories included.</Typography>
                ) : (
                  <Stack spacing={1} sx={{ mb: 2 }}>
                    {preview.included.map((m) => (
                      <Box
                        key={m.id}
                        sx={{
                          display: 'flex', alignItems: 'center', gap: 1,
                          p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider',
                        }}
                      >
                        {m.pinned && (
                          <Tooltip title="Pinned">
                            <Box component="span" sx={{ display: 'inline-flex', color: 'primary.main' }}>
                              <Pin size={13} />
                            </Box>
                          </Tooltip>
                        )}
                        <Box sx={{ flex: 1, minWidth: 0 }}>
                          <Typography variant="body2" noWrap title={m.title}>{m.title || m.id}</Typography>
                          {m.domain && (
                            <Typography variant="caption" color="text.secondary">{m.domain}</Typography>
                          )}
                        </Box>
                        <Chip size="small" color="success" variant="outlined" label={`${Math.round(m.relevance * 100)}%`} />
                      </Box>
                    ))}
                  </Stack>
                )}

                {/* Excluded */}
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
                  Excluded ({preview.excluded.length})
                </Typography>
                {preview.excluded.length === 0 ? (
                  <Typography variant="caption" color="text.secondary">Nothing excluded.</Typography>
                ) : (
                  <Stack spacing={1} sx={{ mb: 2 }}>
                    {preview.excluded.map((m) => (
                      <Box
                        key={m.id}
                        sx={{
                          display: 'flex', alignItems: 'center', gap: 1,
                          p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider', opacity: 0.75,
                        }}
                      >
                        <Box sx={{ flex: 1, minWidth: 0 }}>
                          <Typography variant="body2" noWrap title={m.title}>{m.title || m.id}</Typography>
                          {m.domain && (
                            <Typography variant="caption" color="text.secondary">{m.domain}</Typography>
                          )}
                        </Box>
                        <Chip size="small" variant="outlined" label={`${Math.round(m.relevance * 100)}%`} />
                        <Chip size="small" color="warning" variant="outlined" label={REASON_LABELS[m.reason] ?? m.reason} />
                      </Box>
                    ))}
                  </Stack>
                )}

                {/* Applicable rules */}
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
                  Applicable rules ({preview.applicable_rules.length})
                </Typography>
                {preview.applicable_rules.length === 0 ? (
                  <Typography variant="caption" color="text.secondary">No applicable rules.</Typography>
                ) : (
                  <Stack spacing={0.5} sx={{ mb: 2 }}>
                    {preview.applicable_rules.map((rule) => (
                      <Box key={rule.id} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <Typography variant="body2">{rule.title || rule.id}</Typography>
                        {rule.scope && <Chip size="small" variant="outlined" label={rule.scope} />}
                      </Box>
                    ))}
                  </Stack>
                )}

                {/* Improved prompt */}
                <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>Improved prompt</Typography>
                <Box
                  sx={{
                    p: 1.5, borderRadius: 1, bgcolor: 'rgba(0,0,0,0.3)',
                    border: '1px solid', borderColor: 'divider',
                    fontFamily: 'monospace', fontSize: 12, whiteSpace: 'pre-wrap',
                    maxHeight: 320, overflow: 'auto',
                  }}
                >
                  {preview.improved_prompt || '(empty)'}
                </Box>
              </Box>
            )}
          </CardContent>
        </Card>
      </Box>

      <Snackbar
        open={saved}
        autoHideDuration={2000}
        onClose={() => setSaved(false)}
        message="Saved"
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      />
    </Box>
  )
}
