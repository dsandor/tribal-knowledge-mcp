import { useEffect, useState } from 'react'
import { api, type Cluster } from '@/lib/api'
import { Network } from 'lucide-react'
import Accordion from '@mui/material/Accordion'
import AccordionDetails from '@mui/material/AccordionDetails'
import AccordionSummary from '@mui/material/AccordionSummary'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'

export default function Clusters() {
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  useEffect(() => {
    let ignore = false
    api.clusters.list()
      .then(data => { if (!ignore) setClusters(data) })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
    return () => { ignore = true }
  }, [])

  const toggle = (id: string) =>
    setExpanded(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s })

  if (error) return <Alert severity="error">Error: {error}</Alert>

  return (
    <Stack spacing={2}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Network style={{ width: 20, height: 20, color: '#a78bfa' }} />
        <Typography variant="h6" sx={{ fontWeight: 600 }}>Clusters</Typography>
        <Chip label={clusters.length} size="small" sx={{ ml: 0.5 }} />
      </Box>

      {clusters.length === 0 && (
        <Typography color="text.secondary">
          No clusters yet. Run the analysis pipeline to generate clusters.
        </Typography>
      )}

      <Stack spacing={1}>
        {clusters.map(c => (
          <Accordion
            key={c.ID}
            expanded={expanded.has(c.ID)}
            onChange={() => toggle(c.ID)}
            disableGutters
            elevation={0}
            sx={{
              border: '1px solid',
              borderColor: 'divider',
              borderRadius: '8px !important',
              bgcolor: 'background.paper',
              '&:before': { display: 'none' },
              '&:hover': { borderColor: 'rgba(255,255,255,0.2)' },
            }}
          >
            <AccordionSummary
              expandIcon={<ExpandMoreIcon sx={{ fontSize: 18 }} />}
              sx={{ px: 2, py: 0.5, minHeight: 'unset', '& .MuiAccordionSummary-content': { my: 1 } }}
            >
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>{c.Title}</Typography>
                <Box sx={{ display: 'flex', gap: 2, mt: 0.5 }}>
                  {c.Domain && (
                    <Chip label={c.Domain} size="small" variant="outlined" sx={{ height: 18, fontSize: 11 }} />
                  )}
                  <Typography variant="caption" color="text.secondary">
                    {c.EntryIDs?.length ?? 0} entries
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    quality {(c.QualityScore ?? 0).toFixed(2)}
                  </Typography>
                </Box>
              </Box>
            </AccordionSummary>
            {c.Summary && (
              <AccordionDetails sx={{ px: 2, pb: 2, pt: 0, borderTop: '1px solid', borderColor: 'divider' }}>
                <Typography variant="body2" color="text.secondary" sx={{ pt: 1.5 }}>
                  {c.Summary}
                </Typography>
              </AccordionDetails>
            )}
          </Accordion>
        ))}
      </Stack>
    </Stack>
  )
}
