import { useEffect, useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Box from '@mui/material/Box'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import Typography from '@mui/material/Typography'
import TextField from '@mui/material/TextField'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import Divider from '@mui/material/Divider'
import Alert from '@mui/material/Alert'
import CircularProgress from '@mui/material/CircularProgress'
import { Psychology as BrainIcon, Lock as LockIcon } from '@mui/icons-material'
import { fetchAuthInfo } from '../lib/api'

export default function Login() {
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [oidcEnabled, setOidcEnabled] = useState(false)
  const [loadingInfo, setLoadingInfo] = useState(true)
  // When OIDC is the configured provider, lead with SSO and tuck the admin-key
  // form behind a link (only the superadmin needs it).
  const [showKeyForm, setShowKeyForm] = useState(false)
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string })?.from ?? '/'

  useEffect(() => {
    fetchAuthInfo()
      .then(info => setOidcEnabled(info.oidc_enabled))
      .catch(() => setOidcEnabled(false))
      .finally(() => setLoadingInfo(false))
  }, [])

  const handleSSO = () => {
    // Full-page redirect into the server-driven OIDC flow.
    window.location.href = '/auth/oidc/login'
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const trimmed = key.trim()
      const headers: Record<string, string> = {}
      if (trimmed) headers['Authorization'] = `Bearer ${trimmed}`
      const r = await fetch('/api/me', { headers })
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

  const showKeyEntry = !oidcEnabled || showKeyForm

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

          {loadingInfo ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, py: 3 }}>
              <CircularProgress size={20} />
              <Typography color="text.secondary">Loading…</Typography>
            </Box>
          ) : (
            <>
              {oidcEnabled && (
                <>
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                    Sign in with your organization account to continue.
                  </Typography>
                  <Button
                    variant="contained"
                    size="large"
                    fullWidth
                    onClick={handleSSO}
                  >
                    Sign in with SSO
                  </Button>

                  {!showKeyForm && (
                    <Box sx={{ mt: 3, textAlign: 'center' }}>
                      <Link
                        component="button"
                        type="button"
                        variant="caption"
                        underline="hover"
                        color="text.secondary"
                        onClick={() => setShowKeyForm(true)}
                        sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}
                      >
                        <LockIcon sx={{ fontSize: 14 }} /> Administrator sign-in
                      </Link>
                    </Box>
                  )}

                  {showKeyForm && <Divider sx={{ my: 3 }} />}
                </>
              )}

              {showKeyEntry && (
                <>
                  {!oidcEnabled && (
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
                      Enter your API key to continue. For first-time setup, use the{' '}
                      <code>SUPERADMIN_KEY</code> from your server config or <code>.env</code> file.
                    </Typography>
                  )}

                  <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <TextField
                      label={oidcEnabled ? 'Administrator API key' : 'API Key'}
                      type="password"
                      value={key}
                      onChange={e => setKey(e.target.value)}
                      placeholder="Paste your API key here"
                      autoFocus={!oidcEnabled}
                      autoComplete="current-password"
                      fullWidth
                    />

                    {error && <Alert severity="error">{error}</Alert>}

                    <Button
                      type="submit"
                      variant={oidcEnabled ? 'outlined' : 'contained'}
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
                </>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </Box>
  )
}
