import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Box from '@mui/material/Box'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Typography from '@mui/material/Typography'
import TextField from '@mui/material/TextField'
import Button from '@mui/material/Button'
import Alert from '@mui/material/Alert'
import { Psychology as BrainIcon } from '@mui/icons-material'

export default function Login() {
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string })?.from ?? '/'

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const trimmed = key.trim()
      const headers: Record<string, string> = {}
      if (trimmed) headers['Authorization'] = `Bearer ${trimmed}`
      const r = await fetch('/api/stats', { headers })
      if (r.status === 401) {
        setError('Invalid API key. Check your SUPERADMIN_KEY or team API key.')
        return
      }
      if (!r.ok) {
        setError('Server error — check that the server is running.')
        return
      }
      localStorage.setItem('tkm_api_key', trimmed)
      navigate(from, { replace: true })
    } catch {
      setError('Could not reach the server.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Box
      sx={{
        minHeight: '100vh',
        bgcolor: 'background.default',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        p: 3,
      }}
    >
      <Card sx={{ width: '100%', maxWidth: 400 }}>
        <CardContent sx={{ p: 4 }}>
          {/* Brand */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 4 }}>
            <BrainIcon sx={{ color: 'primary.main', fontSize: 28 }} />
            <Typography variant="h6" sx={{ fontWeight: 600 }}>Tribal Knowledge</Typography>
          </Box>

          <Typography variant="h5" sx={{ fontWeight: 700 }} gutterBottom>Sign in</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
            Enter your API key to continue. For first-time setup, use the{' '}
            <code>SUPERADMIN_KEY</code> from your server config or <code>.env</code> file.
          </Typography>

          <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <TextField
              label="API Key"
              type="password"
              value={key}
              onChange={e => setKey(e.target.value)}
              placeholder="Paste your API key here"
              autoFocus
              autoComplete="current-password"
              fullWidth
            />

            {error && <Alert severity="error">{error}</Alert>}

            <Button
              type="submit"
              variant="contained"
              disabled={busy}
              fullWidth
              size="large"
            >
              {busy ? 'Verifying...' : 'Sign In'}
            </Button>
          </Box>

          <Typography variant="caption" color="text.disabled" sx={{ mt: 3, display: 'block' }}>
            If the server is running with <code>DEV_BYPASS_AUTH=true</code>, leave the field
            empty and click Sign In.
          </Typography>
        </CardContent>
      </Card>
    </Box>
  )
}
