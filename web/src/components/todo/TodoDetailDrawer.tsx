import { useEffect, useState } from 'react'
import Alert from '@mui/material/Alert'
import Autocomplete from '@mui/material/Autocomplete'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Divider from '@mui/material/Divider'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { ExternalLink as ExternalLinkIcon, Trash2, X, Plus, BookOpen } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import {
  TodoItem, TodoList, getTodo, updateTodo, deleteTodo,
  addTodoLink, removeTodoLink, setTodoKnowledgeRefs,
  api, KnowledgeEntry,
} from '@/lib/api'
import { providerLabel, priorityColor } from './todoTheme'

export default function TodoDetailDrawer({ item, lists, onClose, onChanged }: {
  item: TodoItem | null
  lists: TodoList[]
  onClose: () => void
  onChanged: (updated: TodoItem | null) => void
}) {
  const [full, setFull] = useState<TodoItem | null>(null)
  const [draft, setDraft] = useState<TodoItem | null>(null)
  const [linkProvider, setLinkProvider] = useState('jira')
  const [linkExternalID, setLinkExternalID] = useState('')
  const [linkURL, setLinkURL] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Knowledge ref picker (Autocomplete search-by-title, since api.knowledge.list/get exist).
  const [refSearchInput, setRefSearchInput] = useState('')
  const [refOptions, setRefOptions] = useState<KnowledgeEntry[]>([])
  const [refSearchLoading, setRefSearchLoading] = useState(false)
  const [refTitles, setRefTitles] = useState<Record<string, string>>({})

  // Reload the full item (with links + refs) whenever the drawer opens.
  // `ignore` guards against a stale response clobbering state if the user
  // switches to a different todo before this request resolves.
  useEffect(() => {
    if (!item) { setFull(null); setDraft(null); return }
    let ignore = false
    getTodo(item.ID)
      .then(f => { if (!ignore) { setFull(f); setDraft(f) } })
      .catch(() => { if (!ignore) { setFull(item); setDraft(item) } })
    return () => { ignore = true }
  }, [item])

  // Batch-fetch titles for linked knowledge refs so the list shows titles, not raw IDs.
  useEffect(() => {
    const ids = (full?.KnowledgeRefs ?? []).filter(id => !(id in refTitles))
    if (ids.length === 0) return
    let ignore = false
    Promise.all(ids.map(id =>
      api.knowledge.get(id)
        .then(e => [id, e.Title] as const)
        .catch(() => [id, id] as const),
    )).then(pairs => {
      if (ignore) return
      setRefTitles(prev => ({ ...prev, ...Object.fromEntries(pairs) }))
    })
    return () => { ignore = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [full?.KnowledgeRefs])

  // Debounced search-as-you-type for the ref picker.
  useEffect(() => {
    const q = refSearchInput.trim()
    if (q.length < 2) { setRefOptions([]); setRefSearchLoading(false); return }
    setRefSearchLoading(true)
    const handle = setTimeout(() => {
      api.knowledge.list({ search: q, mode: 'hybrid', limit: 8 })
        .then(entries => setRefOptions(entries))
        .catch(() => setRefOptions([]))
        .finally(() => setRefSearchLoading(false))
    }, 300)
    return () => clearTimeout(handle)
  }, [refSearchInput])

  if (!item || !draft) return null

  const save = async () => {
    setError('')
    setBusy(true)
    try {
      const updated = await updateTodo(draft.ID, {
        ListID: draft.ListID, Title: draft.Title, Notes: draft.Notes,
        Status: draft.Status, Priority: draft.Priority, Assignee: draft.Assignee,
        DueDate: draft.DueDate ?? '', // '' clears the due date server-side
        Tags: draft.Tags ?? [],
      })
      setFull(updated); setDraft(updated); onChanged(updated)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!window.confirm('Delete this todo? This cannot be undone.')) return
    setError('')
    setBusy(true)
    try {
      await deleteTodo(draft.ID)
      onChanged(null)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const addLink = async () => {
    if (!linkExternalID.trim() && !linkURL.trim()) return
    setError('')
    setBusy(true)
    try {
      await addTodoLink(draft.ID, {
        Provider: linkProvider, ExternalID: linkExternalID.trim(), URL: linkURL.trim(),
      })
      const refreshed = await getTodo(draft.ID)
      setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
      setLinkExternalID(''); setLinkURL('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const dropLink = async (linkId: string) => {
    setError('')
    setBusy(true)
    try {
      await removeTodoLink(draft.ID, linkId)
      const refreshed = await getTodo(draft.ID)
      setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const addRef = async (id: string) => {
    if (!id || (full?.KnowledgeRefs ?? []).includes(id)) return
    setError('')
    setBusy(true)
    try {
      const next = [...(full?.KnowledgeRefs ?? []), id]
      const refreshed = await setTodoKnowledgeRefs(draft.ID, next)
      setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const dropRef = async (id: string) => {
    setError('')
    setBusy(true)
    try {
      const next = (full?.KnowledgeRefs ?? []).filter(r => r !== id)
      const refreshed = await setTodoKnowledgeRefs(draft.ID, next)
      setFull(refreshed); setDraft(refreshed); onChanged(refreshed)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'operation failed')
    } finally {
      setBusy(false)
    }
  }

  const linkedRefIds = new Set(full?.KnowledgeRefs ?? [])

  return (
    <Drawer anchor="right" open={!!item} onClose={onClose}
      slotProps={{ paper: { sx: { width: 440, p: 2.5, bgcolor: 'background.paper' } } }}>
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
        <Typography sx={{ fontSize: 15, fontWeight: 600, flex: 1 }}>Todo details</Typography>
        <IconButton size="small" onClick={remove} title="Delete todo"><Trash2 size={16} /></IconButton>
        <IconButton size="small" onClick={onClose}><X size={16} /></IconButton>
      </Box>

      {error && (
        <Alert severity="error" onClose={() => setError('')} sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.75 }}>
        <TextField label="Title" size="small" fullWidth value={draft.Title}
          onChange={(e) => setDraft({ ...draft, Title: e.target.value })} />
        <TextField label="Notes (markdown)" size="small" fullWidth multiline minRows={3} value={draft.Notes}
          onChange={(e) => setDraft({ ...draft, Notes: e.target.value })} />
        <Box sx={{ display: 'flex', gap: 1.5 }}>
          <TextField select label="Status" size="small" fullWidth value={draft.Status}
            onChange={(e) => setDraft({ ...draft, Status: e.target.value as TodoItem['Status'] })}>
            <MenuItem value="open">Open</MenuItem>
            <MenuItem value="in_progress">In Progress</MenuItem>
            <MenuItem value="blocked">Blocked</MenuItem>
            <MenuItem value="done">Done</MenuItem>
            <MenuItem value="cancelled">Cancelled</MenuItem>
          </TextField>
          <TextField select label="Priority" size="small" fullWidth value={draft.Priority}
            onChange={(e) => setDraft({ ...draft, Priority: e.target.value as TodoItem['Priority'] })}>
            {(['urgent', 'high', 'medium', 'low'] as const).map(p => (
              <MenuItem key={p} value={p}>
                <Box component="span" sx={{ color: priorityColor(p), textTransform: 'capitalize' }}>{p}</Box>
              </MenuItem>
            ))}
          </TextField>
        </Box>
        <Box sx={{ display: 'flex', gap: 1.5 }}>
          <TextField select label="List" size="small" fullWidth value={draft.ListID}
            onChange={(e) => setDraft({ ...draft, ListID: e.target.value })}>
            {lists.map(l => <MenuItem key={l.ID} value={l.ID}>{l.Name}</MenuItem>)}
          </TextField>
          <TextField label="Assignee" size="small" fullWidth value={draft.Assignee}
            onChange={(e) => setDraft({ ...draft, Assignee: e.target.value })} />
        </Box>
        <TextField label="Due date" type="date" size="small"
          slotProps={{ inputLabel: { shrink: true } }}
          value={draft.DueDate ? draft.DueDate.slice(0, 10) : ''}
          onChange={(e) => setDraft({ ...draft, DueDate: e.target.value ? new Date(e.target.value + 'T12:00:00Z').toISOString() : null })} />
        <TextField label="Tags (comma-separated)" size="small" fullWidth
          value={(draft.Tags ?? []).join(', ')}
          onChange={(e) => setDraft({ ...draft, Tags: e.target.value.split(',').map(t => t.trim()).filter(Boolean) })} />
        <Button variant="contained" size="small" onClick={save} disabled={busy}>Save changes</Button>
      </Box>

      <Divider sx={{ my: 2.5 }} />

      {/* External tracker links */}
      <Typography sx={{ fontSize: 13, fontWeight: 600, mb: 1 }}>Issue tracker links</Typography>
      {(full?.ExternalLinks ?? []).map(l => {
        // Only render as a clickable link when the URL is http(s) — an
        // unvalidated/legacy javascript: URI must never reach an href.
        const safeUrl = /^https?:\/\//i.test(l.URL) ? l.URL : undefined
        return (
          <Box key={l.ID} sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75, p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider' }}>
            <Chip size="small" label={providerLabel(l.Provider)} sx={{ height: 18, fontSize: 10 }} />
            <Typography sx={{ fontSize: 12, flex: 1 }}>{l.ExternalID || l.URL}</Typography>
            {l.ExternalStatus && <Typography sx={{ fontSize: 11 }} color="text.secondary">{l.ExternalStatus}</Typography>}
            {safeUrl && (
              <IconButton size="small" component="a" href={safeUrl} target="_blank" rel="noopener">
                <ExternalLinkIcon size={13} />
              </IconButton>
            )}
            <IconButton size="small" onClick={() => dropLink(l.ID)} disabled={busy}><X size={13} /></IconButton>
          </Box>
        )
      })}
      <Box sx={{ display: 'flex', gap: 1, mt: 1 }}>
        <TextField select size="small" value={linkProvider} onChange={(e) => setLinkProvider(e.target.value)} sx={{ width: 130 }} disabled={busy}>
          {['jira', 'servicenow', 'github', 'gitlab', 'other'].map(p => (
            <MenuItem key={p} value={p}>{providerLabel(p)}</MenuItem>
          ))}
        </TextField>
        <TextField size="small" placeholder="ID (PROJ-123)" value={linkExternalID}
          onChange={(e) => setLinkExternalID(e.target.value)} sx={{ width: 110 }} disabled={busy} />
        <TextField size="small" placeholder="URL" value={linkURL} onChange={(e) => setLinkURL(e.target.value)} sx={{ flex: 1 }} disabled={busy} />
        <IconButton size="small" onClick={addLink} disabled={busy}><Plus size={15} /></IconButton>
      </Box>

      <Divider sx={{ my: 2.5 }} />

      {/* Knowledge refs */}
      <Typography sx={{ fontSize: 13, fontWeight: 600, mb: 1 }}>Linked knowledge</Typography>
      {(full?.KnowledgeRefs ?? []).map(id => (
        <Box key={id} sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.75, p: 1, borderRadius: 1, border: '1px solid', borderColor: 'divider' }}>
          <BookOpen size={13} color="#94a3b8" />
          <Typography component={RouterLink} to={`/knowledge/${id}`} sx={{ fontSize: 12, flex: 1, color: '#93c5fd', textDecoration: 'none', '&:hover': { textDecoration: 'underline' } }}>
            {refTitles[id] ?? id}
          </Typography>
          <IconButton size="small" onClick={() => dropRef(id)} disabled={busy}><X size={13} /></IconButton>
        </Box>
      ))}
      <Autocomplete<KnowledgeEntry>
        size="small"
        sx={{ mt: 1 }}
        disabled={busy}
        options={refOptions.filter(o => !linkedRefIds.has(o.ID))}
        loading={refSearchLoading}
        inputValue={refSearchInput}
        onInputChange={(_, value) => setRefSearchInput(value)}
        value={null}
        onChange={(_, option) => {
          if (!option) return
          setRefSearchInput(''); setRefOptions([])
          addRef(option.ID)
        }}
        getOptionLabel={(o) => o.Title}
        isOptionEqualToValue={(o, v) => o.ID === v.ID}
        filterOptions={(x) => x}
        noOptionsText={refSearchInput.trim().length < 2 ? 'Type to search…' : 'No matching entries'}
        renderInput={(params) => (
          <TextField
            {...params}
            placeholder="Search knowledge entries by title…"
            slotProps={{
              ...params.slotProps,
              input: {
                ...params.slotProps.input,
                endAdornment: (
                  <>
                    {refSearchLoading ? <CircularProgress color="inherit" size={14} /> : null}
                    {params.slotProps.input.endAdornment}
                  </>
                ),
              },
            }}
          />
        )}
      />
    </Drawer>
  )
}
