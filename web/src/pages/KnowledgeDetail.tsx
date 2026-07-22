import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, searchSimilar, addVisibilityRule, getMyTeams, setEntryAuthor, pinEntry, todosForEntry, type KnowledgeEntry, type TodoItem } from '@/lib/api'
import { priorityColor, statusLabel } from '@/components/todo/todoTheme'
import { ArrowLeft, Star, Pencil, Pin, Trash2, EyeOff, UserX, Share2, Copy, Check, X } from 'lucide-react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogContentText from '@mui/material/DialogContentText'
import DialogTitle from '@mui/material/DialogTitle'
import IconButton from '@mui/material/IconButton'
import InputLabel from '@mui/material/InputLabel'
import FormControl from '@mui/material/FormControl'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
import Snackbar from '@mui/material/Snackbar'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { TagPill } from '@/components/ui/tag-pill'
import { MarkdownView } from '@/components/ui/markdown-view'

const KNOWLEDGE_TYPES = [
  'prompt_template',
  'best_practice',
  'checklist',
  'reference',
  'pattern',
  'antipattern',
]

interface EditFields {
  Title: string
  Content: string
  Type: string
  Domain: string
  Tags: string
  Description: string
}

function toEditFields(e: KnowledgeEntry): EditFields {
  return {
    Title: e.Title,
    Content: e.Content,
    Type: e.Type,
    Domain: e.Domain ?? '',
    Tags: e.Tags ? e.Tags.join(', ') : '',
    Description: e.Description ?? '',
  }
}

export default function KnowledgeDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [entry, setEntry] = useState<KnowledgeEntry | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [rating, setRating] = useState(0)
  const [ratingSaving, setRatingSaving] = useState(false)

  // edit mode
  const [editing, setEditing] = useState(false)
  const [editFields, setEditFields] = useState<EditFields | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  // delete confirmation dialog
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  // similar entries
  const [similar, setSimilar] = useState<KnowledgeEntry[]>([])

  // related todos
  const [relatedTodos, setRelatedTodos] = useState<TodoItem[]>([])

  // visibility (hide/mute) feedback
  const [visMsg, setVisMsg] = useState<string | null>(null)

  // share dialog
  const [shareUrl, setShareUrl] = useState<string | null>(null)
  const [sharing, setSharing] = useState(false)

  // team name mapping
  const [teamNames, setTeamNames] = useState<Record<string, string>>({})

  // inline author editing (only when author is empty)
  const [authorEditing, setAuthorEditing] = useState(false)
  const [authorDraft, setAuthorDraft] = useState('')
  const [authorSaving, setAuthorSaving] = useState(false)
  const [authorError, setAuthorError] = useState<string | null>(null)

  const teamName = (tid: string | undefined | null): string => {
    if (!tid) return '—'
    return teamNames[tid] ?? tid
  }

  const refreshEntry = async () => {
    if (!id) return
    const e = await api.knowledge.get(id)
    setEntry(e)
    setRating(e.Rating)
  }

  const saveAuthor = async () => {
    if (!entry) return
    const value = authorDraft.trim()
    if (!value) return
    setAuthorSaving(true)
    setAuthorError(null)
    try {
      await setEntryAuthor(entry.ID, value)
      await refreshEntry()
      setAuthorEditing(false)
      setAuthorDraft('')
    } catch (e) {
      setAuthorError(e instanceof Error ? e.message : 'Set author failed.')
    } finally {
      setAuthorSaving(false)
    }
  }

  const handleShare = async () => {
    if (!entry) return
    setSharing(true)
    try {
      const res = await api.share.create(entry.ID)
      setShareUrl(`${window.location.origin}${res.url}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSharing(false)
    }
  }

  const copyShareUrl = () => {
    if (shareUrl) {
      navigator.clipboard?.writeText(shareUrl).then(
        () => setVisMsg('Share link copied'),
        () => { /* clipboard may be unavailable; the field is still selectable */ },
      )
    }
  }

  const handleHide = async () => {
    if (!entry) return
    try {
      await addVisibilityRule('item', entry.ID)
      setVisMsg('Item hidden from your results')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const handlePin = async () => {
    if (!entry) return
    try {
      await pinEntry(entry.ID)
      setVisMsg('Pinned to enrichment')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const handleMuteAuthor = async () => {
    if (!entry || !entry.Author) return
    try {
      await addVisibilityRule('author', entry.Author)
      setVisMsg(`Muted author "${entry.Author}"`)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    if (!id) return
    let ignore = false
    api.knowledge.get(id)
      .then(e => {
        if (!ignore) {
          setEntry(e)
          setRating(e.Rating)
          searchSimilar(e.ID, e.Title)
            .then(results => { if (!ignore) setSimilar(results) })
            .catch(() => { /* silently ignore */ })
        }
      })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
    return () => { ignore = true }
  }, [id])

  // Related todos linked to this entry. Resilient to failure.
  useEffect(() => {
    if (!id) return
    let ignore = false
    todosForEntry(id)
      .then(results => { if (!ignore) setRelatedTodos(results) })
      .catch(() => { if (!ignore) setRelatedTodos([]) })
    return () => { ignore = true }
  }, [id])

  // Load team names once for the id -> name mapping. Resilient to failure.
  useEffect(() => {
    let ignore = false
    getMyTeams()
      .then(res => {
        if (ignore) return
        setTeamNames(Object.fromEntries((res.teams ?? []).map(t => [t.id, t.name])))
      })
      .catch(() => { /* skip team names on error */ })
    return () => { ignore = true }
  }, [])

  const saveRating = async () => {
    if (!id) return
    setRatingSaving(true)
    try {
      await api.knowledge.rate(id, rating)
      setEntry(prev => prev ? { ...prev, Rating: rating } : prev)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRatingSaving(false)
    }
  }

  const startEdit = () => {
    if (!entry) return
    setEditFields(toEditFields(entry))
    setSaveError(null)
    setEditing(true)
  }

  const cancelEdit = () => {
    setEditing(false)
    setEditFields(null)
    setSaveError(null)
  }

  const handleSave = async () => {
    if (!id || !editFields) return
    setSaving(true)
    setSaveError(null)
    try {
      const tags = editFields.Tags.split(',').map(t => t.trim()).filter(Boolean)
      const updated = await api.knowledge.update(id, {
        Title: editFields.Title,
        Content: editFields.Content,
        Type: editFields.Type,
        Domain: editFields.Domain,
        Tags: tags,
        Description: editFields.Description,
      })
      setEntry(updated)
      setEditing(false)
      setEditFields(null)
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    setDeleting(true)
    try {
      await api.knowledge.delete(id)
      navigate('/knowledge')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  const field = (key: keyof EditFields, value: string) =>
    setEditFields(prev => prev ? { ...prev, [key]: value } : prev)

  if (error) return <Alert severity="error">Error: {error}</Alert>
  if (!entry) return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <CircularProgress size={20} />
      <Typography color="text.secondary">Loading...</Typography>
    </Box>
  )

  // ---- EDIT MODE ----
  if (editing && editFields) {
    return (
      <Stack spacing={2} sx={{ maxWidth: 768 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button variant="text" size="small" startIcon={<ArrowLeft style={{ width: 16, height: 16 }} />} onClick={cancelEdit}>
            Cancel
          </Button>
          <Typography variant="h6" sx={{ fontWeight: 600, flex: 1 }}>Edit Entry</Typography>
        </Box>

        <Card>
          <CardContent>
            <Stack spacing={2} sx={{ pt: 1 }}>
              <TextField
                label="Title"
                fullWidth
                value={editFields.Title}
                onChange={e => field('Title', e.target.value)}
              />

              <FormControl fullWidth size="small">
                <InputLabel>Type</InputLabel>
                <Select
                  label="Type"
                  value={editFields.Type}
                  onChange={e => field('Type', e.target.value)}
                >
                  {KNOWLEDGE_TYPES.map(t => (
                    <MenuItem key={t} value={t}>{t}</MenuItem>
                  ))}
                </Select>
              </FormControl>

              <TextField
                label="Domain"
                fullWidth
                value={editFields.Domain}
                onChange={e => field('Domain', e.target.value)}
              />

              <TextField
                label="Tags (comma-separated)"
                fullWidth
                value={editFields.Tags}
                onChange={e => field('Tags', e.target.value)}
                placeholder="tag1, tag2, tag3"
              />

              {entry.AutoTags && entry.AutoTags.length > 0 && (
                <Box>
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
                    Auto-categorized (read-only)
                  </Typography>
                  <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                    {entry.AutoTags.map(t => (
                      <TagPill key={`auto-${t}`} label={t} variant="auto" />
                    ))}
                  </Box>
                </Box>
              )}

              <TextField
                label="Description"
                fullWidth
                value={editFields.Description}
                onChange={e => field('Description', e.target.value)}
              />

              <TextField
                label="Content"
                fullWidth
                multiline
                minRows={8}
                value={editFields.Content}
                onChange={e => field('Content', e.target.value)}
                slotProps={{ input: { style: { fontFamily: 'monospace' } } }}
              />

              {saveError && <Alert severity="error">{saveError}</Alert>}

              <Box sx={{ display: 'flex', gap: 1 }}>
                <Button variant="contained" onClick={handleSave} disabled={saving}>
                  {saving ? 'Saving...' : 'Save'}
                </Button>
                <Button variant="text" onClick={cancelEdit} disabled={saving}>
                  Cancel
                </Button>
              </Box>
            </Stack>
          </CardContent>
        </Card>
      </Stack>
    )
  }

  // ---- VIEW MODE ----
  return (
    <Stack spacing={2} sx={{ maxWidth: 768 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
        <IconButton size="small" onClick={() => navigate(-1)}>
          <ArrowLeft style={{ width: 16, height: 16 }} />
        </IconButton>
        <Typography variant="h6" sx={{ fontWeight: 600, flex: 1 }}>{entry.Title}</Typography>
        <Chip label={entry.Type} size="small" />
        <IconButton size="small" onClick={handleShare} disabled={sharing} title="Share with another team">
          <Share2 style={{ width: 16, height: 16 }} />
        </IconButton>
        <IconButton size="small" onClick={handlePin} title="Pin to enrichment">
          <Pin style={{ width: 16, height: 16 }} />
        </IconButton>
        <IconButton size="small" onClick={handleHide} title="Hide this item from my results">
          <EyeOff style={{ width: 16, height: 16 }} />
        </IconButton>
        {entry.Author && (
          <IconButton size="small" onClick={handleMuteAuthor} title={`Mute author "${entry.Author}"`}>
            <UserX style={{ width: 16, height: 16 }} />
          </IconButton>
        )}
        <IconButton size="small" onClick={startEdit} title="Edit">
          <Pencil style={{ width: 16, height: 16 }} />
        </IconButton>
        <IconButton size="small" onClick={() => setConfirmDelete(true)} title="Delete" sx={{ color: 'error.main' }}>
          <Trash2 style={{ width: 16, height: 16 }} />
        </IconButton>
      </Box>

      <Snackbar
        open={visMsg !== null}
        autoHideDuration={3000}
        onClose={() => setVisMsg(null)}
        message={visMsg ?? ''}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      />

      {/* Share link dialog */}
      <Dialog open={shareUrl !== null} onClose={() => setShareUrl(null)} fullWidth maxWidth="sm">
        <DialogTitle>Share this knowledge</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ mb: 2 }}>
            Send this single-use link to a teammate on another team. They can import a copy
            into their team once.
          </DialogContentText>
          <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
            <TextField
              fullWidth
              size="small"
              value={shareUrl ?? ''}
              slotProps={{ input: { readOnly: true } }}
              onFocus={(e) => e.target.select()}
            />
            <IconButton onClick={copyShareUrl} title="Copy link">
              <Copy style={{ width: 18, height: 18 }} />
            </IconButton>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShareUrl(null)}>Close</Button>
        </DialogActions>
      </Dialog>

      {/* Delete confirmation dialog */}
      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)}>
        <DialogTitle>Confirm deletion?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            This will permanently delete "{entry.Title}". This action cannot be undone.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDelete(false)} disabled={deleting}>Cancel</Button>
          <Button color="error" variant="contained" onClick={handleDelete} disabled={deleting}>
            {deleting ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>

      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', alignItems: 'center' }}>
        {entry.Domain && (
          <Chip label={entry.Domain} size="small" variant="outlined" />
        )}
        {entry.Author ? (
          <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>{entry.Author}</Typography>
        ) : authorEditing ? (
          <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}>
            <TextField
              value={authorDraft}
              onChange={e => setAuthorDraft(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter') { e.preventDefault(); saveAuthor() }
                if (e.key === 'Escape') { e.preventDefault(); setAuthorEditing(false); setAuthorDraft('') }
              }}
              placeholder="author"
              size="small"
              autoFocus
              disabled={authorSaving}
              sx={{ '& .MuiInputBase-input': { py: 0.25, fontSize: 13, width: 140 } }}
            />
            <IconButton size="small" onClick={saveAuthor} disabled={authorSaving || !authorDraft.trim()} title="Save author">
              <Check style={{ width: 16, height: 16 }} />
            </IconButton>
            <IconButton size="small" onClick={() => { setAuthorEditing(false); setAuthorDraft('') }} disabled={authorSaving} title="Cancel">
              <X style={{ width: 16, height: 16 }} />
            </IconButton>
          </Box>
        ) : (
          <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.25 }}>
            <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center', fontStyle: 'italic' }}>unknown</Typography>
            <IconButton size="small" onClick={() => { setAuthorEditing(true); setAuthorDraft(''); setAuthorError(null) }} title="Set author" sx={{ p: 0.25 }}>
              <Pencil style={{ width: 14, height: 14 }} />
            </IconButton>
          </Box>
        )}
        <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
          · {teamName(entry.TeamID || entry.Team)}
        </Typography>
        {entry.Tags?.map(t => (
          <TagPill key={t} label={t} variant="user" />
        ))}
        {entry.AutoTags?.map(t => (
          <TagPill key={`auto-${t}`} label={t} variant="auto" />
        ))}
        <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
          {new Date(entry.CreatedAt).toLocaleDateString()}
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
          {entry.UsageCount ?? 0} uses
        </Typography>
      </Box>

      {authorError && <Alert severity="error" onClose={() => setAuthorError(null)}>{authorError}</Alert>}

      {entry.Description && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Description</Typography>} sx={{ pb: 0 }} />
          <CardContent>
            <MarkdownView content={entry.Description} />
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader title={<Typography variant="subtitle2">Content</Typography>} sx={{ pb: 0 }} />
        <CardContent>
          <MarkdownView content={entry.Content} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader title={<Typography variant="subtitle2">Rating</Typography>} sx={{ pb: 0 }} />
        <CardContent>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <Box sx={{ display: 'flex', gap: 0.5 }}>
              {[1, 2, 3, 4, 5].map(n => (
                <IconButton key={n} size="small" onClick={() => setRating(n)} disableRipple>
                  <Star
                    style={{
                      width: 24,
                      height: 24,
                      color: n <= rating ? '#fbbf24' : '#475569',
                      fill: n <= rating ? '#fbbf24' : 'none',
                    }}
                  />
                </IconButton>
              ))}
            </Box>
            <Typography variant="body2" color="text.secondary">{(rating ?? 0).toFixed(1)}</Typography>
            <Button size="small" variant="contained" onClick={saveRating} disabled={ratingSaving}>
              {ratingSaving ? 'Saving...' : 'Save Rating'}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {similar.length > 0 && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Similar Entries</Typography>} sx={{ pb: 0 }} />
          <CardContent sx={{ pt: 1 }}>
            <List disablePadding>
              {similar.map(s => (
                <ListItem key={s.ID} disableGutters sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 0.5 }}>
                  <Typography
                    component={Link}
                    to={`/knowledge/${s.ID}`}
                    variant="body2"
                    sx={{
                      flex: 1,
                      minWidth: 0,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      color: 'text.primary',
                      textDecoration: 'none',
                      '&:hover': { color: 'primary.light' },
                    }}
                  >
                    {s.Title}
                  </Typography>
                  {s.Domain && (
                    <Chip label={s.Domain} size="small" variant="outlined" sx={{ flexShrink: 0 }} />
                  )}
                  <Chip label={s.Type} size="small" variant="outlined" sx={{ flexShrink: 0 }} />
                </ListItem>
              ))}
            </List>
          </CardContent>
        </Card>
      )}

      {relatedTodos.length > 0 && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Related Todos</Typography>} sx={{ pb: 0 }} />
          <CardContent sx={{ pt: 1 }}>
            <List disablePadding>
              {relatedTodos.map(t => (
                <ListItem key={t.ID} disableGutters sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 0.5 }}>
                  <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: priorityColor(t.Priority), flexShrink: 0 }} />
                  <Typography
                    component={Link}
                    to="/todos"
                    variant="body2"
                    sx={{
                      flex: 1,
                      minWidth: 0,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      color: 'text.primary',
                      textDecoration: 'none',
                      '&:hover': { color: 'primary.light' },
                    }}
                  >
                    {t.Title}
                  </Typography>
                  <Chip label={statusLabel(t.Status)} size="small" variant="outlined" sx={{ flexShrink: 0 }} />
                </ListItem>
              ))}
            </List>
          </CardContent>
        </Card>
      )}
    </Stack>
  )
}
