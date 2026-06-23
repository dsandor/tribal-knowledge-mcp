import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, type SharePreview, type ShareImportResult } from '@/lib/api'
import { Download, Users } from 'lucide-react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { MarkdownView } from '@/components/ui/markdown-view'

export default function ShareLanding() {
  const { token } = useParams<{ token: string }>()

  const [preview, setPreview] = useState<SharePreview | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [importing, setImporting] = useState(false)
  const [result, setResult] = useState<ShareImportResult | null>(null)
  const [importError, setImportError] = useState<string | null>(null)

  useEffect(() => {
    if (!token) return
    let ignore = false
    setLoading(true)
    api.share.get(token)
      .then(p => { if (!ignore) setPreview(p) })
      .catch(e => { if (!ignore) setLoadError(e instanceof Error ? e.message : String(e)) })
      .finally(() => { if (!ignore) setLoading(false) })
    return () => { ignore = true }
  }, [token])

  const handleImport = async () => {
    if (!token) return
    setImporting(true)
    setImportError(null)
    try {
      const res = await api.share.import(token)
      setResult(res)
    } catch (e) {
      setImportError(e instanceof Error ? e.message : String(e))
    } finally {
      setImporting(false)
    }
  }

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
        <CircularProgress size={28} />
      </Box>
    )
  }

  if (loadError || !preview) {
    return (
      <Box sx={{ maxWidth: 720, mx: 'auto' }}>
        <Alert severity="error">{loadError ?? 'Share not found.'}</Alert>
        <Box sx={{ mt: 2 }}>
          <Button component={Link} to="/knowledge">Back to knowledge</Button>
        </Box>
      </Box>
    )
  }

  const canImport = preview.importable && !preview.already_yours && result?.status !== 'pending'

  return (
    <Stack spacing={2} sx={{ maxWidth: 720, mx: 'auto' }}>
      <Box>
        <Typography variant="overline" color="text.secondary">Shared knowledge</Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
          <Typography variant="h6" sx={{ fontWeight: 600, flex: 1 }}>{preview.title}</Typography>
          {preview.type && <Chip label={preview.type} size="small" />}
        </Box>
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', alignItems: 'center', mt: 1 }}>
          {preview.domain && <Chip label={preview.domain} size="small" variant="outlined" />}
          {preview.author && (
            <Typography variant="caption" color="text.secondary">by {preview.author}</Typography>
          )}
          {preview.tags?.map(t => (
            <Chip key={t} label={t} size="small" variant="outlined" />
          ))}
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mt: 1 }}>
          <Users style={{ width: 14, height: 14 }} />
          <Typography variant="caption" color="text.secondary">
            Shared from team {preview.source_team_id || 'unknown'}
          </Typography>
        </Box>
      </Box>

      <Card>
        <CardHeader title={<Typography variant="subtitle2">Content</Typography>} sx={{ pb: 0 }} />
        <CardContent>
          <MarkdownView content={preview.content} />
        </CardContent>
      </Card>

      {/* Import outcomes */}
      {result?.status === 'pending' && (
        <Alert severity="success" action={<Button component={Link} to="/pending" size="small">View pending</Button>}>
          Imported into your team. It is awaiting curator approval in your pending queue.
        </Alert>
      )}
      {(result?.status === 'already_yours' || preview.already_yours) && (
        <Alert severity="info">This knowledge already belongs to your team.</Alert>
      )}
      {importError && <Alert severity="error">{importError}</Alert>}
      {!preview.importable && !preview.already_yours && !result && (
        <Alert severity="warning">This share link has already been used or was revoked.</Alert>
      )}

      <Box>
        <Button
          variant="contained"
          startIcon={<Download style={{ width: 16, height: 16 }} />}
          onClick={handleImport}
          disabled={!canImport || importing}
        >
          {importing ? 'Importing…' : 'Import to my team'}
        </Button>
      </Box>
    </Stack>
  )
}
