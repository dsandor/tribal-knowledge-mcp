import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, copyKnowledge, getMe, getMyTeams, moveKnowledge, setEntryAuthor, type KnowledgeEntry } from '@/lib/api'
import { Check, ChevronLeft, ChevronRight, Pencil, Search, Tag, X } from 'lucide-react'
import { TagPill } from '@/components/ui/tag-pill'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControl from '@mui/material/FormControl'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormGroup from '@mui/material/FormGroup'
import IconButton from '@mui/material/IconButton'
import InputLabel from '@mui/material/InputLabel'
import MenuItem from '@mui/material/MenuItem'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import Snackbar from '@mui/material/Snackbar'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'

const PAGE_SIZE = 20
const TYPES = ['', 'prompt', 'pattern', 'workflow', 'domain_fact', 'anti_pattern']

type SearchMode = 'hybrid' | 'semantic' | 'keyword'

const MODE_LABELS: Record<SearchMode, string> = {
  hybrid: 'Hybrid',
  semantic: 'Semantic',
  keyword: 'Keyword',
}

const MODE_HINTS: Partial<Record<SearchMode, string>> = {
  semantic: 'Searching by meaning — great for concepts and synonyms',
  keyword: 'Searching by exact terms — great for specific phrases',
}

const MODE_BADGES: Partial<Record<SearchMode, string>> = {
  semantic: 'Semantic search',
  keyword: 'Keyword search',
}

function Highlight({ text, query }: { text: string; query: string }) {
  if (!query.trim()) return <>{text}</>

  const terms = query
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map(t => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))

  if (terms.length === 0) return <>{text}</>

  const pattern = new RegExp(`(${terms.join('|')})`, 'gi')
  const parts = text.split(pattern)

  return (
    <>
      {parts.map((part, i) =>
        part.match(new RegExp(terms.join('|'), 'i'))
          ? <mark key={i} style={{ background: 'rgba(251,191,36,0.2)', color: '#fde68a', borderRadius: 2, padding: '0 2px' }}>{part}</mark>
          : <span key={i}>{part}</span>
      )}
    </>
  )
}

function getSnippet(content: string, query: string, maxLen = 150): string {
  if (!query.trim()) return content.slice(0, maxLen)

  const terms = query.trim().split(/\s+/).filter(Boolean)
  const lower = content.toLowerCase()

  let earliest = content.length
  for (const t of terms) {
    const idx = lower.indexOf(t.toLowerCase())
    if (idx !== -1 && idx < earliest) earliest = idx
  }

  if (earliest === content.length) return content.slice(0, maxLen)

  const start = Math.max(0, earliest - 40)
  const end = Math.min(content.length, start + maxLen)
  const snippet = content.slice(start, end)
  return (start > 0 ? '…' : '') + snippet + (end < content.length ? '…' : '')
}

export default function KnowledgeBrowser() {
  const [entries, setEntries] = useState<KnowledgeEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [domain, setDomain] = useState('')
  const [domainInput, setDomainInput] = useState('')
  const [type, setType] = useState('')
  const [searchMode, setSearchMode] = useState<SearchMode>('hybrid')
  const [tagFilter, setTagFilter] = useState('')

  // --- Superadmin bulk move/copy ---
  const [isSuperadmin, setIsSuperadmin] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [teams, setTeams] = useState<{ id: string; name: string }[]>([])
  const [moveOpen, setMoveOpen] = useState(false)
  const [copyOpen, setCopyOpen] = useState(false)
  const [moveTarget, setMoveTarget] = useState('')
  const [copyTargets, setCopyTargets] = useState<Set<string>>(new Set())
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkError, setBulkError] = useState<string | null>(null)
  const [toast, setToast] = useState<string | null>(null)

  // --- Team name mapping (all users) ---
  const [teamNames, setTeamNames] = useState<Record<string, string>>({})

  // --- Inline author editing (entries with empty author) ---
  const [authorEditId, setAuthorEditId] = useState<string | null>(null)
  const [authorDraft, setAuthorDraft] = useState('')
  const [authorSaving, setAuthorSaving] = useState(false)

  const teamName = (id: string | undefined | null): string => {
    if (!id) return '—'
    return teamNames[id] ?? id
  }

  const fetchEntries = useCallback(() => {
    let ignore = false
    setLoading(true)
    setError(null)
    api.knowledge
      .list({ limit: PAGE_SIZE, offset: page * PAGE_SIZE, search, domain, type, mode: searchMode, tag: tagFilter || undefined })
      .then(entries => { if (!ignore) setEntries(entries) })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
      .finally(() => { if (!ignore) setLoading(false) })
    return () => { ignore = true }
  }, [page, search, domain, type, searchMode, tagFilter])

  useEffect(() => fetchEntries(), [fetchEntries])

  // Determine superadmin once; load teams for the pickers and for the
  // id -> name mapping used in card meta lines.
  useEffect(() => {
    let ignore = false
    getMyTeams()
      .then(res => {
        if (ignore) return
        const list = res.teams ?? []
        setTeams(list)
        setTeamNames(Object.fromEntries(list.map(t => [t.id, t.name])))
      })
      .catch(() => { /* unauth or error: skip team names, don't crash */ })
    getMe()
      .then(me => { if (!ignore && me.role === 'superadmin') setIsSuperadmin(true) })
      .catch(() => { /* non-superadmin or unauthenticated: leave page unchanged */ })
    return () => { ignore = true }
  }, [])

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const clearSelection = () => setSelected(new Set())

  const selectedIds = Array.from(selected)

  // Select-all state across the currently-rendered page of entries.
  const allSelected = entries.length > 0 && entries.every(e => selected.has(e.ID))
  const someSelected = entries.some(e => selected.has(e.ID))

  const toggleSelectAll = () => {
    setSelected(prev => {
      if (entries.length > 0 && entries.every(e => prev.has(e.ID))) {
        // all currently visible are selected -> deselect them
        const next = new Set(prev)
        entries.forEach(e => next.delete(e.ID))
        return next
      }
      // otherwise select all visible
      const next = new Set(prev)
      entries.forEach(e => next.add(e.ID))
      return next
    })
  }

  const startAuthorEdit = (id: string) => {
    setAuthorEditId(id)
    setAuthorDraft('')
    setBulkError(null)
  }

  const cancelAuthorEdit = () => {
    setAuthorEditId(null)
    setAuthorDraft('')
  }

  const saveAuthor = async (id: string) => {
    const value = authorDraft.trim()
    if (!value) return
    setAuthorSaving(true)
    try {
      await setEntryAuthor(id, value)
      setAuthorEditId(null)
      setAuthorDraft('')
      fetchEntries()
      setToast('Author set.')
    } catch (e) {
      setBulkError(e instanceof Error ? e.message : 'Set author failed.')
    } finally {
      setAuthorSaving(false)
    }
  }

  const handleMoveConfirm = async () => {
    if (!moveTarget || selectedIds.length === 0) return
    setBulkBusy(true)
    setBulkError(null)
    try {
      await moveKnowledge(selectedIds, moveTarget)
      const count = selectedIds.length
      setMoveOpen(false)
      setMoveTarget('')
      clearSelection()
      fetchEntries()
      setToast(`Moved ${count} entr${count !== 1 ? 'ies' : 'y'}.`)
    } catch (e) {
      setBulkError(e instanceof Error ? e.message : 'Move failed.')
    } finally {
      setBulkBusy(false)
    }
  }

  const handleCopyConfirm = async () => {
    const targets = Array.from(copyTargets)
    if (targets.length === 0 || selectedIds.length === 0) return
    setBulkBusy(true)
    setBulkError(null)
    try {
      await copyKnowledge(selectedIds, targets)
      const count = selectedIds.length
      setCopyOpen(false)
      setCopyTargets(new Set())
      clearSelection()
      fetchEntries()
      setToast(`Copied ${count} entr${count !== 1 ? 'ies' : 'y'} to ${targets.length} team${targets.length !== 1 ? 's' : ''}.`)
    } catch (e) {
      setBulkError(e instanceof Error ? e.message : 'Copy failed.')
    } finally {
      setBulkBusy(false)
    }
  }

  const toggleCopyTarget = (id: string) => {
    setCopyTargets(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleSearch = () => { setSearch(searchInput); setPage(0) }

  const handleModeChange = (_: React.MouseEvent<HTMLElement>, mode: SearchMode | null) => {
    if (mode !== null) {
      setSearchMode(mode)
      setPage(0)
    }
  }

  if (error) return <Alert severity="error">Error: {error}</Alert>

  return (
    <Stack spacing={3}>
      <Typography variant="h6" sx={{ fontWeight: 600 }}>Knowledge Browser</Typography>

      <Stack spacing={1.5}>
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
          <Box sx={{ display: 'flex', gap: 1, flex: 1, minWidth: 256 }}>
            <TextField
              placeholder="Search title or content…"
              value={searchInput}
              onChange={e => setSearchInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSearch()}
              sx={{ flex: 1 }}
            />
            <IconButton onClick={handleSearch} sx={{ border: '1px solid', borderColor: 'divider' }}>
              <Search style={{ width: 16, height: 16 }} />
            </IconButton>
          </Box>
          <Select
            value={type}
            onChange={e => { setType(e.target.value); setPage(0) }}
            size="small"
            displayEmpty
            sx={{ minWidth: 140 }}
          >
            {TYPES.map(t => (
              <MenuItem key={t} value={t}>{t || 'All types'}</MenuItem>
            ))}
          </Select>
          <TextField
            placeholder="Domain filter"
            value={domainInput}
            onChange={e => setDomainInput(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') { setDomain(domainInput); setPage(0) } }}
            sx={{ width: 160 }}
          />
        </Box>

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <ToggleButtonGroup
            value={searchMode}
            exclusive
            onChange={handleModeChange}
            size="small"
          >
            {(['hybrid', 'semantic', 'keyword'] as SearchMode[]).map(mode => (
              <ToggleButton key={mode} value={mode} sx={{ px: 2, py: 0.5, fontSize: 12 }}>
                {MODE_LABELS[mode]}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
          {tagFilter && (
            <Chip
              icon={<Tag style={{ width: 12, height: 12 }} />}
              label={`tag: ${tagFilter}`}
              size="small"
              onDelete={() => { setTagFilter(''); setPage(0) }}
              sx={{ bgcolor: 'rgba(16, 185, 129, 0.18)', color: '#34d399' }}
            />
          )}
          {search !== '' && MODE_HINTS[searchMode] && (
            <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
              {MODE_HINTS[searchMode]}
            </Typography>
          )}
        </Box>
      </Stack>

      {isSuperadmin && selected.size > 0 && (
        <Paper
          variant="outlined"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1.5,
            px: 2,
            py: 1,
            position: 'sticky',
            top: 0,
            zIndex: 2,
            bgcolor: 'rgba(16, 185, 129, 0.12)',
          }}
        >
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            {selected.size} selected
          </Typography>
          <Box sx={{ flex: 1 }} />
          <Button size="small" variant="outlined" onClick={() => { setBulkError(null); setMoveTarget(''); setMoveOpen(true) }}>
            Move to team…
          </Button>
          <Button size="small" variant="outlined" onClick={() => { setBulkError(null); setCopyTargets(new Set()); setCopyOpen(true) }}>
            Copy to teams…
          </Button>
          <Button size="small" onClick={clearSelection}>Clear</Button>
        </Paper>
      )}

      {loading ? (
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 4 }}>
          <CircularProgress size={24} />
        </Box>
      ) : (
        <Stack spacing={1}>
          {search !== '' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Typography variant="caption" color="text.secondary">
                {entries.length === 0 ? 'No results' : `${entries.length} result${entries.length !== 1 ? 's' : ''}`}
              </Typography>
              {MODE_BADGES[searchMode] && (
                <Chip label={MODE_BADGES[searchMode]} size="small" variant="outlined" sx={{ height: 18, fontSize: 10 }} />
              )}
            </Box>
          )}
          {entries.length === 0 && (
            <Typography color="text.secondary">No entries found.</Typography>
          )}
          {isSuperadmin && entries.length > 0 && (
            <FormControlLabel
              sx={{ ml: 0, mb: 0.5 }}
              control={
                <Checkbox
                  size="small"
                  checked={allSelected}
                  indeterminate={!allSelected && someSelected}
                  onChange={toggleSelectAll}
                  slotProps={{ input: { 'aria-label': 'Select all entries on this page' } }}
                />
              }
              label={<Typography variant="caption" color="text.secondary">Select all</Typography>}
            />
          )}
          {entries.map(e => {
            const snippet = getSnippet(e.Content ?? '', search)
            const card = (
              <Card
                key={e.ID}
                component={Link}
                to={`/knowledge/${e.ID}`}
                sx={{
                  flex: 1,
                  minWidth: 0,
                  textDecoration: 'none',
                  cursor: 'pointer',
                  '&:hover': { borderColor: 'primary.main' },
                }}
              >
                <CardContent sx={{ py: 1.5, px: 2, display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 2 }}>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography
                      variant="body2"
                      sx={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    >
                      {search ? <Highlight text={e.Title} query={search} /> : e.Title}
                    </Typography>
                    <Box sx={{ mt: 0.5, display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 0.5 }}>
                      <Typography variant="caption" color="text.secondary">
                        {e.Domain || 'no domain'} ·{' '}
                      </Typography>
                      {e.Author ? (
                        <Typography variant="caption" color="text.secondary">{e.Author}</Typography>
                      ) : authorEditId === e.ID ? (
                        <Box
                          sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}
                          onClick={ev => { ev.preventDefault(); ev.stopPropagation() }}
                        >
                          <TextField
                            value={authorDraft}
                            onChange={ev => setAuthorDraft(ev.target.value)}
                            onKeyDown={ev => {
                              if (ev.key === 'Enter') { ev.preventDefault(); saveAuthor(e.ID) }
                              if (ev.key === 'Escape') { ev.preventDefault(); cancelAuthorEdit() }
                            }}
                            placeholder="author"
                            size="small"
                            autoFocus
                            disabled={authorSaving}
                            sx={{ '& .MuiInputBase-input': { py: 0.25, fontSize: 12, width: 110 } }}
                          />
                          <IconButton size="small" onClick={() => saveAuthor(e.ID)} disabled={authorSaving || !authorDraft.trim()} title="Save author">
                            <Check style={{ width: 14, height: 14 }} />
                          </IconButton>
                          <IconButton size="small" onClick={cancelAuthorEdit} disabled={authorSaving} title="Cancel">
                            <X style={{ width: 14, height: 14 }} />
                          </IconButton>
                        </Box>
                      ) : (
                        <Box
                          sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.25 }}
                          onClick={ev => { ev.preventDefault(); ev.stopPropagation() }}
                        >
                          <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>unknown</Typography>
                          <IconButton size="small" onClick={() => startAuthorEdit(e.ID)} title="Set author" sx={{ p: 0.25 }}>
                            <Pencil style={{ width: 12, height: 12 }} />
                          </IconButton>
                        </Box>
                      )}
                      <Typography variant="caption" color="text.secondary">
                        · {teamName(e.TeamID || e.Team)} · ★ {(e.Rating ?? 0).toFixed(1)}
                      </Typography>
                    </Box>
                    {(() => {
                      const user = e.Tags ?? []
                      const auto = e.AutoTags ?? []
                      const MAX = 5
                      const pills = [
                        ...user.map(t => ({ t, v: 'user' as const })),
                        ...auto.map(t => ({ t, v: 'auto' as const })),
                      ]
                      if (pills.length === 0) return null
                      const overflow = pills.length - MAX
                      return (
                        <Box
                          sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', mt: 0.75 }}
                          onClick={ev => { ev.preventDefault(); ev.stopPropagation() }}
                        >
                          {pills.slice(0, MAX).map(({ t, v }) => (
                            <TagPill key={`${v}-${t}`} label={t} variant={v}
                              onClick={() => { setTagFilter(t); setPage(0) }} />
                          ))}
                          {overflow > 0 && (
                            <Chip label={`+${overflow}`} size="small" variant="outlined"
                              sx={{ height: 20, fontSize: 10 }} />
                          )}
                        </Box>
                      )
                    })()}
                    {search && snippet && (
                      <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                        <Highlight text={snippet} query={search} />
                      </Typography>
                    )}
                  </Box>
                  <Chip label={e.Type} size="small" sx={{ flexShrink: 0 }} />
                </CardContent>
              </Card>
            )
            if (!isSuperadmin) return card
            return (
              <Box key={e.ID} sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <Checkbox
                  checked={selected.has(e.ID)}
                  onChange={() => toggleSelect(e.ID)}
                  size="small"
                  slotProps={{ input: { 'aria-label': `Select ${e.Title}` } }}
                />
                {card}
              </Box>
            )
          })}
        </Stack>
      )}

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
        <Button
          variant="outlined"
          size="small"
          onClick={() => setPage(p => Math.max(0, p - 1))}
          disabled={page === 0}
          startIcon={<ChevronLeft style={{ width: 16, height: 16 }} />}
        >
          Prev
        </Button>
        <Typography variant="body2" color="text.secondary">Page {page + 1}</Typography>
        <Button
          variant="outlined"
          size="small"
          onClick={() => setPage(p => p + 1)}
          disabled={entries.length < PAGE_SIZE}
          endIcon={<ChevronRight style={{ width: 16, height: 16 }} />}
        >
          Next
        </Button>
      </Box>

      {/* Move to team dialog (single team) */}
      <Dialog open={moveOpen} onClose={bulkBusy ? undefined : () => setMoveOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Move {selected.size} entr{selected.size !== 1 ? 'ies' : 'y'} to team</DialogTitle>
        <DialogContent>
          <FormControl fullWidth size="small" sx={{ mt: 1 }}>
            <InputLabel>Team</InputLabel>
            <Select
              label="Team"
              value={moveTarget}
              onChange={ev => setMoveTarget(ev.target.value)}
              disabled={bulkBusy}
            >
              {teams.map(t => (
                <MenuItem key={t.id} value={t.id}>{t.name}</MenuItem>
              ))}
            </Select>
          </FormControl>
          {bulkError && <Alert severity="error" sx={{ mt: 2 }}>{bulkError}</Alert>}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setMoveOpen(false)} disabled={bulkBusy}>Cancel</Button>
          <Button variant="contained" onClick={handleMoveConfirm} disabled={bulkBusy || !moveTarget}>
            {bulkBusy ? 'Moving…' : 'Move'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Copy to teams dialog (multiple teams) */}
      <Dialog open={copyOpen} onClose={bulkBusy ? undefined : () => setCopyOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Copy {selected.size} entr{selected.size !== 1 ? 'ies' : 'y'} to teams</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            Copies appear as pending in each selected team.
          </Typography>
          <FormGroup>
            {teams.map(t => (
              <FormControlLabel
                key={t.id}
                control={
                  <Checkbox
                    checked={copyTargets.has(t.id)}
                    onChange={() => toggleCopyTarget(t.id)}
                    disabled={bulkBusy}
                    size="small"
                  />
                }
                label={t.name}
              />
            ))}
          </FormGroup>
          {bulkError && <Alert severity="error" sx={{ mt: 2 }}>{bulkError}</Alert>}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCopyOpen(false)} disabled={bulkBusy}>Cancel</Button>
          <Button variant="contained" onClick={handleCopyConfirm} disabled={bulkBusy || copyTargets.size === 0}>
            {bulkBusy ? 'Copying…' : 'Copy'}
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={toast !== null}
        autoHideDuration={3000}
        onClose={() => setToast(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        message={toast ?? ''}
      />
    </Stack>
  )
}
