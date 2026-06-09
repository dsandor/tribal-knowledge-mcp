import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Agent } from '@/lib/api'
import { Bot, Download } from 'lucide-react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import type { ChipProps } from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
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

export default function Agents() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let ignore = false
    api.agents.list()
      .then(data => { if (!ignore) setAgents(data) })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
    return () => { ignore = true }
  }, [])

  if (error) return <Alert severity="error">Error: {error}</Alert>

  return (
    <Stack spacing={2}>
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Bot style={{ width: 20, height: 20, color: '#34d399' }} />
          <Typography variant="h6" sx={{ fontWeight: 600 }}>Agents</Typography>
          <Chip label={agents.length} size="small" sx={{ ml: 0.5 }} />
        </Box>
        {agents.length > 0 && (
          <Button
            variant="outlined"
            size="small"
            startIcon={<Download style={{ width: 16, height: 16 }} />}
            onClick={() => api.agents.bulkDownload()}
          >
            Bulk Export (ZIP)
          </Button>
        )}
      </Box>

      {agents.length === 0 && (
        <Typography color="text.secondary">
          No agents generated yet. The pipeline creates agents from clusters.
        </Typography>
      )}

      <Stack spacing={1}>
        {agents.map(a => (
          <Card
            key={a.ID}
            component={Link}
            to={`/agents/${a.ID}`}
            sx={{
              textDecoration: 'none',
              cursor: 'pointer',
              '&:hover': { borderColor: 'primary.main' },
            }}
          >
            <CardContent sx={{ py: 1.5, px: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <Box>
                <Typography variant="body2" sx={{ fontWeight: 500, textTransform: 'capitalize' }}>
                  {a.Domain}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                  v{a.Version} · updated {new Date(a.UpdatedAt).toLocaleDateString()}
                </Typography>
              </Box>
              <Chip label={a.Status} size="small" color={typeColor(a.Status)} />
            </CardContent>
          </Card>
        ))}
      </Stack>
    </Stack>
  )
}
