import { useEffect, useState } from 'react'
import { api, type DatasetSnapshot } from '@/lib/api'
import { Database, Download } from 'lucide-react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Chip from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'

export default function Datasets() {
  const [snaps, setSnaps] = useState<DatasetSnapshot[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let ignore = false
    api.datasets.list()
      .then(data => { if (!ignore) setSnaps(data) })
      .catch(e => { if (!ignore) setError(e instanceof Error ? e.message : String(e)) })
    return () => { ignore = true }
  }, [])

  const download = (id: string, format: 'json' | 'csv') => {
    api.datasets.download(id, format)
  }

  if (error) return <Alert severity="error">Error: {error}</Alert>

  return (
    <Stack spacing={2}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Database style={{ width: 20, height: 20, color: '#22d3ee' }} />
        <Typography variant="h6" sx={{ fontWeight: 600 }}>Datasets</Typography>
        <Chip label={snaps.length} size="small" sx={{ ml: 0.5 }} />
      </Box>

      {snaps.length === 0 && (
        <Typography color="text.secondary">No dataset snapshots yet.</Typography>
      )}

      <Stack spacing={1}>
        {snaps.map(s => (
          <Card key={s.ID}>
            <CardContent sx={{ py: 1.5, px: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <Box>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>Snapshot v{s.Version}</Typography>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
                  {s.EntryCount} entries · {s.ClusterCount} clusters · {new Date(s.CreatedAt).toLocaleString()}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', gap: 1 }}>
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<Download style={{ width: 14, height: 14 }} />}
                  onClick={() => download(s.ID, 'json')}
                >
                  JSON
                </Button>
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<Download style={{ width: 14, height: 14 }} />}
                  onClick={() => download(s.ID, 'csv')}
                >
                  CSV
                </Button>
              </Box>
            </CardContent>
          </Card>
        ))}
      </Stack>
    </Stack>
  )
}
