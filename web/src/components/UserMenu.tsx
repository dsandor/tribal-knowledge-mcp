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
import { Check, EyeOff, KeyRound, LogOut, Users } from 'lucide-react'
import { getMe, getMyTeams, getActiveTeam, setActiveTeam, logout } from '@/lib/api'

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

  const handleOpen = (e: MouseEvent<HTMLElement>) => setAnchorEl(e.currentTarget)
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
  // Map the home team id to its human-readable name; fall back to the raw id.
  const homeTeamName = teams.find((t) => t.id === me?.team_id)?.name ?? me?.team_id

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

        {/* 3. Links */}
        <MenuItem onClick={() => { handleClose(); navigate('/my-visibility') }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><EyeOff size={15} /></ListItemIcon>
          <ListItemText primary="My Visibility" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
        <MenuItem onClick={() => { handleClose(); navigate('/api-keys') }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><KeyRound size={15} /></ListItemIcon>
          <ListItemText primary="My API Keys" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>

        <Divider />

        {/* 4. Sign out */}
        <MenuItem onClick={() => { handleClose(); handleSignOut() }} dense>
          <ListItemIcon sx={{ minWidth: 28 }}><LogOut size={15} /></ListItemIcon>
          <ListItemText primary="Sign Out" slotProps={{ primary: { style: { fontSize: 13 } } }} />
        </MenuItem>
      </Menu>
    </Box>
  )
}
