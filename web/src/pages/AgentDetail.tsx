import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, type Agent, type AgentVersion } from '@/lib/api'
import { ArrowLeft, Download, CheckCircle, Wand2, Pencil, Check, X } from 'lucide-react'
import type { ChipProps } from '@mui/material/Chip'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Collapse from '@mui/material/Collapse'
import IconButton from '@mui/material/IconButton'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

function typeColor(type: string): ChipProps['color'] {
  switch (type) {
    case 'published': return 'success'
    case 'draft': return 'warning'
    case 'prompt_template':
    case 'prompt': return 'primary'
    case 'best_practice':
    case 'pattern': return 'secondary'
    case 'anti_pattern':
    case 'antipattern': return 'error'
    case 'checklist': return 'info'
    default: return 'default'
  }
}

export default function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [agent, setAgent] = useState<Agent | null>(null)
  const [versions, setVersions] = useState<AgentVersion[]>([])
  const [error, setError] = useState<string | null>(null)
  const [publishing, setPublishing] = useState(false)
  const [refactorOpen, setRefactorOpen] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [refactoring, setRefactoring] = useState(false)
  const [refactorError, setRefactorError] = useState<string | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [newDomain, setNewDomain] = useState('')
  const [renameSubmitting, setRenameSubmitting] = useState(false)
  const [renameError, setRenameError] = useState<string | null>(null)
  const [renameMsg, setRenameMsg] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    let ignore = false
    api.agents.get(id)
      .then(({ agent, versions }) => {
        if (!ignore) { setAgent(agent); setVersions(versions) }
      })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
    return () => { ignore = true }
  }, [id])

  const reload = (agentId: string) =>
    api.agents.get(agentId).then(({ agent, versions }) => {
      setAgent(agent)
      setVersions(versions)
    })

  const refactor = async () => {
    if (!id || !feedback.trim()) return
    setRefactoring(true)
    setRefactorError(null)
    try {
      await api.agents.refactor(id, feedback.trim())
      setFeedback('')
      setRefactorOpen(false)
      await reload(id)
    } catch (e) {
      setRefactorError(e instanceof Error ? e.message : String(e))
    } finally {
      setRefactoring(false)
    }
  }

  const startRename = () => {
    if (!agent) return
    setNewDomain(agent.Domain)
    setRenameError(null)
    setRenameMsg(null)
    setRenaming(true)
  }

  const submitRename = async () => {
    if (!id || !agent) return
    const next = newDomain.trim()
    if (!next || next === agent.Domain) { setRenaming(false); return }
    setRenameSubmitting(true)
    setRenameError(null)
    try {
      const res = await api.agents.rename(id, next)
      setRenaming(false)
      setRenameMsg(
        `Renamed to "${res.new_domain}" — updated ${res.updated.entries} entries, ${res.updated.clusters} clusters, ${res.updated.agents} agents.`,
      )
      await reload(id)
    } catch (e) {
      setRenameError(e instanceof Error ? e.message : String(e))
    } finally {
      setRenameSubmitting(false)
    }
  }

  const publish = async () => {
    if (!id) return
    setPublishing(true)
    try {
      await api.agents.publish(id)
      setAgent(prev => prev ? { ...prev, Status: 'published' } : prev)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setPublishing(false)
    }
  }

  if (error) return <Alert severity="error">Error: {error}</Alert>
  if (!agent) return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <CircularProgress size={20} />
      <Typography color="text.secondary">Loading...</Typography>
    </Box>
  )

  return (
    <Stack spacing={2} sx={{ maxWidth: 768 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
        <IconButton size="small" onClick={() => navigate(-1)}>
          <ArrowLeft style={{ width: 16, height: 16 }} />
        </IconButton>
        {renaming ? (
          <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', gap: 1, minWidth: 240 }}>
            <TextField
              size="small"
              autoFocus
              fullWidth
              label="Domain name"
              value={newDomain}
              onChange={e => setNewDomain(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter') submitRename()
                if (e.key === 'Escape') setRenaming(false)
              }}
              disabled={renameSubmitting}
              helperText="Renames this domain across all associated entries, clusters, and agents."
            />
            <IconButton size="small" color="success" onClick={submitRename} disabled={renameSubmitting}>
              {renameSubmitting ? <CircularProgress size={16} color="inherit" /> : <Check style={{ width: 16, height: 16 }} />}
            </IconButton>
            <IconButton size="small" onClick={() => setRenaming(false)} disabled={renameSubmitting}>
              <X style={{ width: 16, height: 16 }} />
            </IconButton>
          </Box>
        ) : (
          <>
            <Typography variant="h6" sx={{ textTransform: 'capitalize', fontWeight: 600 }}>
              {agent.Domain} Agent
            </Typography>
            <Tooltip title="Rename domain">
              <IconButton size="small" onClick={startRename}>
                <Pencil style={{ width: 14, height: 14 }} />
              </IconButton>
            </Tooltip>
            <Box sx={{ flex: 1 }} />
          </>
        )}
        <Chip label={agent.Status} size="small" color={typeColor(agent.Status)} />
        <Typography variant="caption" color="text.secondary">v{agent.Version}</Typography>
      </Box>

      {renameError && <Alert severity="error" onClose={() => setRenameError(null)}>{renameError}</Alert>}
      {renameMsg && <Alert severity="success" onClose={() => setRenameMsg(null)}>{renameMsg}</Alert>}

      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
        {agent.Status === 'draft' && (
          <Button
            variant="contained"
            color="success"
            size="small"
            startIcon={<CheckCircle style={{ width: 16, height: 16 }} />}
            onClick={publish}
            disabled={publishing}
          >
            {publishing ? 'Publishing...' : 'Publish Agent'}
          </Button>
        )}
        <Button
          variant="outlined"
          size="small"
          color={refactorOpen ? 'primary' : 'inherit'}
          startIcon={<Wand2 style={{ width: 14, height: 14 }} />}
          onClick={() => { setRefactorOpen(o => !o); setRefactorError(null) }}
        >
          Refactor
        </Button>
        {(['md', 'txt', 'json'] as const).map(fmt => (
          <Button
            key={fmt}
            variant="outlined"
            size="small"
            startIcon={<Download style={{ width: 14, height: 14 }} />}
            onClick={() => api.agents.download(agent.ID, fmt)}
          >
            .{fmt}
          </Button>
        ))}
      </Box>

      <Collapse in={refactorOpen}>
        <Card sx={{ border: '1px solid', borderColor: 'primary.main', borderRadius: 2 }}>
          <CardHeader
            title={<Typography variant="subtitle2">Refactor Agent with Feedback</Typography>}
            subheader={
              <Typography variant="caption" color="text.secondary">
                Describe what should change. The LLM will revise the agent using your feedback and the existing knowledge entries.
              </Typography>
            }
            sx={{ pb: 1 }}
          />
          <CardContent sx={{ pt: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              multiline
              minRows={3}
              maxRows={8}
              fullWidth
              placeholder="e.g. This agent is too narrow — it should cover all product reviews, not just the specific items previously seen. Broaden the scope to any consumer product."
              value={feedback}
              onChange={e => setFeedback(e.target.value)}
              disabled={refactoring}
            />
            {refactorError && <Alert severity="error">{refactorError}</Alert>}
            <Box sx={{ display: 'flex', gap: 1 }}>
              <Button
                variant="contained"
                size="small"
                onClick={refactor}
                disabled={refactoring || feedback.trim().length < 5}
                startIcon={refactoring ? <CircularProgress size={14} color="inherit" /> : <Wand2 style={{ width: 14, height: 14 }} />}
              >
                {refactoring ? 'Refactoring...' : 'Apply Feedback'}
              </Button>
              <Button
                variant="text"
                size="small"
                onClick={() => { setRefactorOpen(false); setFeedback(''); setRefactorError(null) }}
                disabled={refactoring}
              >
                Cancel
              </Button>
            </Box>
          </CardContent>
        </Card>
      </Collapse>

      <Card>
        <CardHeader title={<Typography variant="subtitle2">System Prompt</Typography>} sx={{ pb: 0 }} />
        <CardContent>
          <Typography
            component="pre"
            variant="body2"
            color="text.secondary"
            sx={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', lineHeight: 1.6, m: 0 }}
          >
            {agent.SystemPrompt || '—'}
          </Typography>
        </CardContent>
      </Card>

      {agent.Instructions && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Instructions</Typography>} sx={{ pb: 0 }} />
          <CardContent>
            <Typography
              component="pre"
              variant="body2"
              color="text.secondary"
              sx={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', lineHeight: 1.6, m: 0 }}
            >
              {agent.Instructions}
            </Typography>
          </CardContent>
        </Card>
      )}

      {agent.AntiPatterns && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Anti-Patterns</Typography>} sx={{ pb: 0 }} />
          <CardContent>
            <Typography
              component="pre"
              variant="body2"
              color="text.secondary"
              sx={{ whiteSpace: 'pre-wrap', fontFamily: 'monospace', lineHeight: 1.6, m: 0 }}
            >
              {agent.AntiPatterns}
            </Typography>
          </CardContent>
        </Card>
      )}

      {agent.SourceRefs && agent.SourceRefs.length > 0 && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Source Knowledge Refs</Typography>} sx={{ pb: 0 }} />
          <CardContent>
            <List disablePadding>
              {agent.SourceRefs.map(ref => (
                <ListItem key={ref} disableGutters sx={{ py: 0.25 }}>
                  <Typography variant="caption" sx={{ fontFamily: 'monospace' }} color="text.secondary">
                    {ref}
                  </Typography>
                </ListItem>
              ))}
            </List>
          </CardContent>
        </Card>
      )}

      {versions.length > 0 && (
        <Card>
          <CardHeader title={<Typography variant="subtitle2">Version History</Typography>} sx={{ pb: 0 }} />
          <CardContent>
            <List disablePadding>
              {versions.map(v => (
                <ListItem
                  key={v.ID}
                  disableGutters
                  sx={{
                    display: 'block',
                    borderLeft: '2px solid',
                    borderColor: 'divider',
                    pl: 2,
                    mb: 1.5,
                    '&:last-child': { mb: 0 },
                  }}
                >
                  <Typography variant="caption" sx={{ fontWeight: 500, display: 'block' }} color="text.primary">
                    v{v.Version} — {new Date(v.CreatedAt).toLocaleDateString()}
                  </Typography>
                  {v.Changelog && (
                    <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'pre-wrap', display: 'block', mt: 0.5 }}>
                      {v.Changelog}
                    </Typography>
                  )}
                </ListItem>
              ))}
            </List>
          </CardContent>
        </Card>
      )}
    </Stack>
  )
}
