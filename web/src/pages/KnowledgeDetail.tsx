import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, searchSimilar, type KnowledgeEntry } from '@/lib/api'
import { ArrowLeft, Star, Pencil, Trash2 } from 'lucide-react'
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
        <IconButton size="small" onClick={startEdit} title="Edit">
          <Pencil style={{ width: 16, height: 16 }} />
        </IconButton>
        <IconButton size="small" onClick={() => setConfirmDelete(true)} title="Delete" sx={{ color: 'error.main' }}>
          <Trash2 style={{ width: 16, height: 16 }} />
        </IconButton>
      </Box>

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

      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
        {entry.Domain && (
          <Chip label={entry.Domain} size="small" variant="outlined" />
        )}
        {entry.Author && (
          <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>{entry.Author}</Typography>
        )}
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
    </Stack>
  )
}
