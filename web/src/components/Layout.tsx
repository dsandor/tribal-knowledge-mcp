import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import Box from '@mui/material/Box'
import Drawer from '@mui/material/Drawer'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import Divider from '@mui/material/Divider'
import Typography from '@mui/material/Typography'
import {
  LayoutDashboard, BookOpen, Upload, Network, Database,
  Bot, BarChart2, Clock, Settings, Users, ShieldCheck, LogOut, Activity, KeyRound,
} from 'lucide-react'
import { Psychology as BrainIcon } from '@mui/icons-material'
import { logout } from '@/lib/api'

const DRAWER_WIDTH = 220

const nav = [
  { to: '/dashboard', label: 'Dashboard', Icon: LayoutDashboard },
  { to: '/knowledge', label: 'Knowledge', Icon: BookOpen },
  { to: '/import', label: 'Import', Icon: Upload },
  { to: '/clusters', label: 'Clusters', Icon: Network },
  { to: '/pipeline', label: 'Pipeline', Icon: Activity },
  { to: '/datasets', label: 'Datasets', Icon: Database },
  { to: '/agents', label: 'Agents', Icon: Bot },
  { to: '/analytics', label: 'Analytics', Icon: BarChart2 },
  { to: '/pending', label: 'Pending Queue', Icon: Clock },
  { to: '/settings', label: 'Settings', Icon: Settings },
  { to: '/api-keys', label: 'API Keys', Icon: KeyRound },
  { to: '/users', label: 'Users', Icon: Users },
  { to: '/admin/teams', label: 'Teams', Icon: Users },
  { to: '/admin/users', label: 'All Users', Icon: Users },
  { to: '/admin/auth', label: 'Auth Config', Icon: ShieldCheck },
]

export default function Layout() {
  const { pathname } = useLocation()
  const navigate = useNavigate()

  const handleSignOut = async () => {
    // Clear the server session (OIDC/local) as well as the local Bearer key.
    try { await logout() } catch { /* best effort */ }
    localStorage.removeItem('tkm_api_key')
    navigate('/login')
  }

  return (
    <Box sx={{ display: 'flex', height: '100vh', bgcolor: 'background.default' }}>
      <Drawer
        variant="permanent"
        sx={{
          width: DRAWER_WIDTH,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: DRAWER_WIDTH,
            boxSizing: 'border-box',
            bgcolor: 'background.paper',
            borderRight: '1px solid',
            borderColor: 'divider',
          },
        }}
      >
        {/* Brand header */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 2.5, py: 2, borderBottom: '1px solid', borderColor: 'divider' }}>
          <BrainIcon sx={{ color: 'primary.main', fontSize: 22 }} />
          <Typography variant="body2" sx={{ fontWeight: 600 }} color="text.primary">
            Tribal Knowledge
          </Typography>
        </Box>

        {/* Nav items */}
        <List dense sx={{ flex: 1, py: 1, px: 1 }}>
          {nav.map(({ to, label, Icon }) => {
            const active = pathname.startsWith(to)
            return (
              <ListItemButton
                key={to}
                component={Link}
                to={to}
                selected={active}
                sx={{
                  borderRadius: 1,
                  mb: 0.25,
                  '&.Mui-selected': {
                    bgcolor: 'rgba(255,255,255,0.08)',
                    '&:hover': { bgcolor: 'rgba(255,255,255,0.12)' },
                  },
                }}
              >
                <ListItemIcon sx={{ minWidth: 32, color: active ? 'text.primary' : 'text.secondary' }}>
                  <Icon size={16} />
                </ListItemIcon>
                <ListItemText
                  primary={label}
                  slotProps={{
                    primary: {
                      style: {
                        fontSize: 13,
                        fontWeight: active ? 600 : 400,
                        color: active ? '#e2e8f0' : '#94a3b8',
                      },
                    },
                  }}
                />
              </ListItemButton>
            )
          })}
        </List>

        <Divider />

        {/* Sign out */}
        <List dense sx={{ py: 1, px: 1 }}>
          <ListItemButton onClick={handleSignOut} sx={{ borderRadius: 1 }}>
            <ListItemIcon sx={{ minWidth: 32, color: 'text.secondary' }}>
              <LogOut size={16} />
            </ListItemIcon>
            <ListItemText
              primary="Sign Out"
              slotProps={{
                primary: {
                  style: { fontSize: 13, color: '#94a3b8' },
                },
              }}
            />
          </ListItemButton>
        </List>
      </Drawer>

      {/* Main content */}
      <Box component="main" sx={{ flex: 1, overflow: 'auto', p: 3 }}>
        <Outlet />
      </Box>
    </Box>
  )
}
