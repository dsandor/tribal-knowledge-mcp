import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type KnowledgeEntry } from '@/lib/api'
import { ChevronLeft, ChevronRight, Search } from 'lucide-react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import IconButton from '@mui/material/IconButton'
import MenuItem from '@mui/material/MenuItem'
import Select from '@mui/material/Select'
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

  useEffect(() => {
    let ignore = false
    setLoading(true)
    api.knowledge
      .list({ limit: PAGE_SIZE, offset: page * PAGE_SIZE, search, domain, type, mode: searchMode })
      .then(entries => { if (!ignore) setEntries(entries) })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
      .finally(() => { if (!ignore) setLoading(false) })
    return () => { ignore = true }
  }, [page, search, domain, type, searchMode])

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
          {search !== '' && MODE_HINTS[searchMode] && (
            <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
              {MODE_HINTS[searchMode]}
            </Typography>
          )}
        </Box>
      </Stack>

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
          {entries.map(e => {
            const snippet = getSnippet(e.Content ?? '', search)
            return (
              <Card
                key={e.ID}
                component={Link}
                to={`/knowledge/${e.ID}`}
                sx={{
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
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                      {e.Domain || 'no domain'} · {e.Author || 'unknown'} · ★ {(e.Rating ?? 0).toFixed(1)}
                    </Typography>
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
    </Stack>
  )
}
