import { useEffect, useState, type MouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import Avatar from '@mui/material/Avatar'
import Box from '@mui/material/Box'
import IconButton from '@mui/material/IconButton'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import Divider from '@mui/material/Divider'
import Typography from '@mui/material/Typography'
import Tooltip from '@mui/material/Tooltip'
import Chip from '@mui/material/Chip'
import Snackbar from '@mui/material/Snackbar'
import MCPSetupDialog from '@/components/MCPSetupDialog'
import { copyToClipboard } from '@/lib/clipboard'
import { Check, Copy, EyeOff, KeyRound, LogOut, Plug, Users } from 'lucide-react'
import { getMe, getMyTeams, getActiveTeam, setActiveTeam, listMyAPIKeys, logout, type APIKey } from '@/lib/api'

interface Me { user_id: string; team_id: string; role: string; display: string }
interface Team { id: string; name: string }

// Derive avatar initials from a display name (falls back to "?").
function initials(name?: string): string {
  if (!name) return '?'
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

export default function UserMenu() {
  const navigate = useNavigate()
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null)
  const [me, setMe] = useState<Me | null>(null)
  const [teams, setTeams] = useState<Team[]>([])
  const [activeTeam, setActiveTeamState] = useState<string | null>(getActiveTeam())
  const [keys, setKeys] = useState<APIKey[]>([])
  const [keyState, setKeyState] = useState<'idle' | 'loading' | 'loaded' | 'error'>('idle')
  const [mcpOpen, setMcpOpen] = useState(false)
  const [snack, setSnack] = useState<string | null>(null)
  const open = Boolean(anchorEl)

  // Load profile + teams once on mount so the avatar can render initials,
  // and the menu has data ready when opened.
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [m, t] = await Promise.all([getMe(), getMyTeams()])
        if (cancelled) return
        setMe(m)
        setTeams(t.teams ?? [])
        // Prefer the explicit client-side active team; fall back to server's notion.
        setActiveTeamState(getActiveTeam() ?? t.active_team ?? null)
      } catch {
        /* best effort — avatar still renders with a fallback */
      }
    })()
    return () => { cancelled = true }
  }, [])

  const handleOpen = (e: MouseEvent<HTMLElement>) => {
    setAnchorEl(e.currentTarget)
    // Lazy-load keys the first time the menu opens; retry on next open if the
    // last attempt failed.
    if (keyState === 'idle' || keyState === 'error') {
      setKeyState('loading')
      listMyAPIKeys()
        .then((ks) => { setKeys(ks); setKeyState('loaded') })
        .catch(() => setKeyState('error'))
    }
  }

  const handleCopyKey = async (raw: string) => {
    const ok = await copyToClipboard(raw)
    setSnack(ok ? 'Copied to clipboard' : 'Copy failed')
  }

  const handleClose = () => setAnchorEl(null)

  const handleSelectTeam = (id: string | null) => {
    setActiveTeam(id)
    window.location.reload()
  }

  const handleSignOut = async () => {
    // Clear the server session (OIDC/local) as well as the local Bearer key.
    try { await logout() } catch { /* best effort */ }
    localStorage.removeItem('tkm_api_key')
    navigate('/login')
  }

  const isSuperadmin = me?.role === 'superadmin'
  const seeingAllTeams = isSuperadmin && activeTeam === null
  // Only admin/superadmin may reach the admin-only /api-keys management page.
  const canManageKeys = me?.role === 'admin' || me?.role === 'superadmin'
  // Map the home team id to its human-readable name; fall back to the raw id.
  const homeTeamName = teams.find((t) => t.id === me?.team_id)?.name ?? me?.team_id

  // Show the caller's personal keys; fall back to team keys when they have none.
  const myKeys = keys.filter((k) => k.key_type === 'user' && k.user_id === me?.user_id)
  const displayKeys = (myKeys.length > 0 ? myKeys : keys.filter((k) => k.key_type === 'team')).slice(0, 3)

  return (
    <Box sx={{ px: 1, py: 1 }}>
      <Tooltip title={me?.display ?? 'Account'} placement="right">
        <IconButton
          onClick={handleOpen}
          size="small"
          sx={{ p: 0.5, borderRadius: 1, width: '100%', justifyContent: 'flex-start', gap: 1 }}
        >
          <Avatar sx={{ width: 30, height: 30, bgcolor: 'primary.main', fontSize: 13, fontWeight: 600 }}>
            {initials(me?.display)}
          </Avatar>
          <Box sx={{ minWidth: 0, textAlign: 'left' }}>
            <Typography variant="body2" noWrap sx={{ fontSize: 13, fontWeight: 600, color: '#e2e8f0' }}>
              {me?.display ?? 'Account'}
            </Typography>
            {me?.role && (
              <Typography variant="caption" noWrap sx={{ fontSize: 11, color: '#94a3b8', display: 'block' }}>
                {me.role}
              </Typography>
            )}
          </Box>
        </IconButton>
      </Tooltip>

      <Menu
        anchorEl={anchorEl}
        open={open}
        onClose={handleClose}
        anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
        transformOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{
          paper: {
            sx: {
              minWidth: 260,
              bgcolor: 'background.paper',
              border: '1px solid',
              borderColor: 'divider',
            },
          },
        }}
      >
        {/* 1. Inline profile header */}
        <Box sx={{ px: 2, py: 1.5 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
            <Avatar sx={{ width: 36, height: 36, bgcolor: 'primary.main', fontSize: 14, fontWeight: 600 }}>
              {initials(me?.display)}
            </Avatar>
            <Box sx={{ minWidth: 0 }}>
              <Typography variant="body2" noWrap sx={{ fontWeight: 600, color: 'text.primary' }}>
                {me?.display ?? 'Account'}
              </Typography>
              {me?.role && (
                <Chip
                  label={me.role}
                  size="small"
                  sx={{ height: 18, fontSize: 10, mt: 0.25, bgcolor: 'rgba(255,255,255,0.08)', color: 'text.secondary' }}
                />
              )}
            </Box>
          </Box>
          {me?.team_id && (
            <Typography variant="caption" sx={{ display: 'block', mt: 1, color: 'text.secondary', fontSize: 11 }}>
              Home team: {homeTeamName}
            </Typography>
          )}
        </Box>

        <Divider />

        {/* 2. Active team switcher */}
        <Typography variant="caption" sx={{ px: 2, pt: 1, pb: 0.5, display: 'block', color: 'text.secondary', fontSize: 10, textTransform: 'uppercase', letterSpacing: 0.5 }}>
          Active team
        </Typography>
        {isSuperadmin && (
          <MenuItem onClick={() => handleSelectTeam(null)} selected={seeingAllTeams} dense>
            <ListItemIcon sx={{ minWidth: 28 }}>
              {seeingAllTeams ? <Check size={15} /> : <Users size={15} />}
            </ListItemIcon>
            <ListItemText primary="All teams" slotProps={{ primary: { style: { fontSize: 13 } } }} />
          </MenuItem>
        )}
        {teams.map((t) => {
          const isActive = !seeingAllTeams && activeTeam === t.id
          return (
            <MenuItem key={t.id} onClick={() => handleSelectTeam(t.id)} selected={isActive} dense>
              <ListItemIcon sx={{ minWidth: 28 }}>
                {isActive ? <Check size={15} /> : <Box sx={{ width: 15 }} />}
              </ListItemIcon>
              <ListItemText primary={t.name} slotProps={{ primary: { style: { fontSize: 13 } } }} />
            </MenuItem>
          )
        })}

        <Divider />

        {/* 3. API keys */}
        <Typography variant="caption" sx={{ px: 2, pt: 1, pb: 0.5, display: 'block', color: 'text.secondary', fontSize: 10, textTransform: 'uppercase', letterSpacing: 0.5 }}>
          API keys
        </Typography>
        {keyState === 'loaded' && displayKeys.length === 0 && (
          canManageKeys ? (
            <MenuItem onClick={() => { handleClose(); navigate('/api-keys') }} dense>
              <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
              <ListItemText primary="No API keys yet — create one" slotProps={{ primary: { style: { fontSize: 13 } } }} />
            </MenuItem>
          ) : (
            <MenuItem disabled dense>
              <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
              <ListItemText primary="No API keys yet — ask a team admin" slotProps={{ primary: { style: { fontSize: 13 } } }} />
            </MenuItem>
          )
        )}
        {displayKeys.map((k) => (
          <MenuItem
            key={k.id}
            onClick={() => { if (k.raw_key) handleCopyKey(k.raw_key) }}
            disabled={!k.raw_key}
            dense
          >
            <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
            <ListItemText
              primary={k.name}
              secondary={k.raw_key ? `tk_••••${k.raw_key.slice(-4)}` : 'tk_•••• (not retrievable)'}
              slotProps={{
                primary: { style: { fontSize: 13 } },
                secondary: { style: { fontSize: 11, fontFamily: 'monospace' } },
              }}
            />
            {k.raw_key && <Copy size={13} style={{ opacity: 0.6, flexShrink: 0 }} />}
          </MenuItem>
        ))}
        {canManageKeys && (
          <MenuItem onClick={() => { handleClose(); navigate('/api-keys') }} dense>
            <ListItemIcon sx={{ minWidth: 28 }}><Box sx={{ width: 15 }} /></ListItemIcon>
            <ListItemText primary="Manage keys →" slotProps={{ primary: { style: { fontSize: 12, color: '#94a3b8' } } }} />
          </MenuItem>
        )}

        <Divider />

        {/* 4. Links */}
        <MenuItem onClick={() => { handleClose(); navigate('/my-visibility') }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><EyeOff size={15} /></ListItemIcon>
          <ListItemText primary="My Visibility" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
        <MenuItem onClick={() => { handleClose(); setMcpOpen(true) }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><Plug size={15} /></ListItemIcon>
          <ListItemText primary="MCP Setup" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>

        <Divider />

        {/* 5. Sign out */}
        <MenuItem onClick={() => { handleClose(); handleSignOut() }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><LogOut size={15} /></ListItemIcon>
          <ListItemText primary="Sign Out" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
      </Menu>

      <MCPSetupDialog
        open={mcpOpen}
        onClose={() => setMcpOpen(false)}
        keys={keys}
        meUserId={me?.user_id}
        canManageKeys={canManageKeys}
        onCopied={(ok) => setSnack(ok ? 'Copied to clipboard' : 'Copy failed')}
      />
      <Snackbar
        open={snack !== null}
        autoHideDuration={2000}
        onClose={() => setSnack(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
        message={snack ?? ''}
      />
    </Box>
  )
}
