import { useEffect, useState } from 'react'
import {
  listVisibility,
  addVisibilityRule,
  deleteVisibilityRule,
  type VisibilityRule,
  type VisibilityRuleType,
} from '../lib/api'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import CircularProgress from '@mui/material/CircularProgress'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Alert from '@mui/material/Alert'
import FormControl from '@mui/material/FormControl'
import InputLabel from '@mui/material/InputLabel'
import Select from '@mui/material/Select'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'

// The mute form only offers author/tag/domain. Hidden items ("item" rules) are
// created from the knowledge browser/detail "Hide" action, not entered by hand.
const MUTE_KINDS: { value: VisibilityRuleType; label: string }[] = [
  { value: 'author', label: 'Author' },
  { value: 'tag', label: 'Tag' },
  { value: 'domain', label: 'Domain' },
]

const GROUPS: { type: VisibilityRuleType; title: string; empty: string }[] = [
  { type: 'item', title: 'Hidden items', empty: 'No hidden items.' },
  { type: 'author', title: 'Muted authors', empty: 'No muted authors.' },
  { type: 'tag', title: 'Muted tags', empty: 'No muted tags.' },
  { type: 'domain', title: 'Muted domains', empty: 'No muted domains.' },
]

export default function MyVisibility() {
  const [rules, setRules] = useState<VisibilityRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Add-mute form state
  const [kind, setKind] = useState<VisibilityRuleType>('author')
  const [value, setValue] = useState('')
  const [adding, setAdding] = useState(false)

  const load = () => {
    setLoading(true)
    listVisibility()
      .then((data) => setRules(Array.isArray(data) ? data : []))
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const handleAdd = async () => {
    const v = value.trim()
    if (!v) return
    setAdding(true)
    setError(null)
    try {
      await addVisibilityRule(kind, v)
      setValue('')
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setAdding(false)
    }
  }

  const handleRemove = async (rule: VisibilityRule) => {
    setError(null)
    try {
      await deleteVisibilityRule(rule.rule_type, rule.value)
      setRules((prev) => prev.filter((r) => !(r.rule_type === rule.rule_type && r.value === rule.value)))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <Box sx={{ p: 3, maxWidth: '48rem' }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 1 }}>
        <Typography variant="h5" sx={{ fontWeight: 700 }}>My Visibility</Typography>
        {!loading && (
          <Chip label={`${rules.length} ${rules.length === 1 ? 'rule' : 'rules'}`} size="small" />
        )}
      </Box>
      <Typography color="text.secondary" variant="body2" sx={{ mb: 3 }}>
        These rules hide knowledge from your own results only. They never affect other team members.
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {/* Add-mute form */}
      <Paper sx={{ p: 2, mb: 3, border: '1px solid', borderColor: 'divider' }}>
        <Typography variant="subtitle2" sx={{ mb: 1.5 }}>Mute a source</Typography>
        <Box sx={{ display: 'flex', gap: 1.5, alignItems: 'center', flexWrap: 'wrap' }}>
          <FormControl size="small" sx={{ minWidth: 140 }}>
            <InputLabel id="mute-kind-label">Kind</InputLabel>
            <Select
              labelId="mute-kind-label"
              label="Kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as VisibilityRuleType)}
            >
              {MUTE_KINDS.map((k) => (
                <MenuItem key={k.value} value={k.value}>{k.label}</MenuItem>
              ))}
            </Select>
          </FormControl>
          <TextField
            size="small"
            label="Value"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') handleAdd() }}
            sx={{ flex: 1, minWidth: 200 }}
          />
          <Button variant="contained" onClick={handleAdd} disabled={adding || value.trim() === ''}>
            {adding ? 'Adding...' : 'Mute'}
          </Button>
        </Box>
      </Paper>

      {loading && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, py: 4 }}>
          <CircularProgress size={20} />
          <Typography color="text.secondary">Loading...</Typography>
        </Box>
      )}

      {!loading && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          {GROUPS.map((group) => {
            const items = rules.filter((r) => r.rule_type === group.type)
            return (
              <Box key={group.type}>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  {group.title} {items.length > 0 && <Typography component="span" color="text.secondary" variant="caption">({items.length})</Typography>}
                </Typography>
                {items.length === 0 ? (
                  <Typography color="text.secondary" variant="body2">{group.empty}</Typography>
                ) : (
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                    {items.map((r) => {
                      const isItem = r.rule_type === 'item'
                      return (
                        <Paper
                          key={`${r.rule_type}:${r.value}`}
                          sx={{
                            p: 1.5,
                            display: 'flex',
                            alignItems: 'center',
                            gap: 2,
                            border: '1px solid',
                            borderColor: 'divider',
                          }}
                        >
                          {isItem ? (
                            <Box sx={{ flex: 1, minWidth: 0 }}>
                              <Typography variant="body2" sx={{ fontWeight: 600, wordBreak: 'break-word' }}>
                                {r.title || r.value}
                              </Typography>
                              {r.description && (
                                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-word' }}>
                                  {r.description}
                                </Typography>
                              )}
                              <Typography
                                variant="caption"
                                color="text.disabled"
                                sx={{
                                  display: 'block',
                                  mt: 0.5,
                                  fontFamily: 'monospace',
                                  fontSize: '0.68rem',
                                  opacity: 0.45,
                                  letterSpacing: 0,
                                  wordBreak: 'break-all',
                                }}
                              >
                                {r.value}
                              </Typography>
                            </Box>
                          ) : (
                            <Typography sx={{ flex: 1, minWidth: 0, wordBreak: 'break-all' }} variant="body2">
                              {r.value}
                            </Typography>
                          )}
                          <Button size="small" color="error" onClick={() => handleRemove(r)}>
                            {isItem ? 'Unhide' : 'Remove'}
                          </Button>
                        </Paper>
                      )
                    })}
                  </Box>
                )}
              </Box>
            )
          })}
        </Box>
      )}
    </Box>
  )
}
