import { useRef, useState } from 'react'
import { api } from '../lib/api'
import Box from '@mui/material/Box'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Typography from '@mui/material/Typography'
import Button from '@mui/material/Button'
import Alert from '@mui/material/Alert'
import Divider from '@mui/material/Divider'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'

type Feedback = { severity: 'success' | 'info' | 'error'; message: string }

export default function BackupRestore() {
  const [busy, setBusy] = useState(false)
  const [force, setForce] = useState(false)
  const [backupFeedback, setBackupFeedback] = useState<Feedback | null>(null)
  const [restoreFeedback, setRestoreFeedback] = useState<Feedback | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const handleDownload = async () => {
    setBusy(true)
    setBackupFeedback(null)
    try {
      await api.admin.downloadBackup()
      setBackupFeedback({
        severity: 'info',
        message: 'Backup downloaded. Treat this file as a secret — it contains API keys and auth config.',
      })
    } catch (err) {
      setBackupFeedback({
        severity: 'error',
        message: err instanceof Error ? err.message : 'Backup download failed.',
      })
    } finally {
      setBusy(false)
    }
  }

  const handleRestore = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    // Reset the input so selecting the same file again re-triggers onChange.
    if (fileInputRef.current) fileInputRef.current.value = ''
    if (!file) return

    setBusy(true)
    setRestoreFeedback(null)
    try {
      const result = await api.admin.restore(file, force)
      setRestoreFeedback({
        severity: 'success',
        message: `Restore complete: ${result.embeddings_restored} embeddings restored.`,
      })
    } catch (err) {
      setRestoreFeedback({
        severity: 'error',
        message: err instanceof Error ? err.message : 'Restore failed.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card sx={{ mb: 3 }}>
      <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          Backup &amp; Restore
        </Typography>
        <Typography variant="body2" color="text.secondary">
          Downloads a full archive of this server — all teams, knowledge, agents, users, and
          settings. The archive contains secrets (API keys and auth config); store it securely.
        </Typography>

        <Box>
          <Button variant="outlined" onClick={handleDownload} disabled={busy}>
            Download backup
          </Button>
        </Box>

        {backupFeedback && (
          <Alert severity={backupFeedback.severity} onClose={() => setBackupFeedback(null)}>
            {backupFeedback.message}
          </Alert>
        )}

        <Divider />

        <Typography variant="body2" color="text.secondary">
          Restore from a previously downloaded archive. With force overwrite enabled, this database
          is wiped before the archive is restored.
        </Typography>

        <FormControlLabel
          control={
            <Checkbox
              checked={force}
              onChange={e => setForce(e.target.checked)}
              disabled={busy}
            />
          }
          label="Force overwrite (wipe this database before restoring)"
        />

        <Box>
          <Button variant="outlined" color="warning" component="label" disabled={busy}>
            {busy ? 'Working…' : 'Restore from archive'}
            <input
              ref={fileInputRef}
              type="file"
              accept=".gz,.tar.gz"
              hidden
              onChange={handleRestore}
            />
          </Button>
        </Box>

        {restoreFeedback && (
          <Alert severity={restoreFeedback.severity} onClose={() => setRestoreFeedback(null)}>
            {restoreFeedback.message}
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}
