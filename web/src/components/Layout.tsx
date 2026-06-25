import { useEffect, useState } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
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
  Bot, BarChart2, Clock, Settings, Users, ShieldCheck, Activity,
  SlidersHorizontal,
} from 'lucide-react'
import { Psychology as BrainIcon } from '@mui/icons-material'
import { getMe } from '@/lib/api'
import UserMenu from './UserMenu'

const DRAWER_WIDTH = 220

// Always visible to every authenticated member.
const baseNav = [
  { to: '/dashboard', label: 'Dashboard', Icon: LayoutDashboard },
  { to: '/knowledge', label: 'Knowledge', Icon: BookOpen },
  { to: '/import', label: 'Import', Icon: Upload },
  { to: '/clusters', label: 'Clusters', Icon: Network },
  { to: '/pipeline', label: 'Pipeline', Icon: Activity },
  { to: '/datasets', label: 'Datasets', Icon: Database },
  { to: '/agents', label: 'Agents', Icon: Bot },
  { to: '/analytics', label: 'Analytics', Icon: BarChart2 },
  { to: '/enrichment', label: 'Enrichment', Icon: SlidersHorizontal },
  { to: '/pending', label: 'Pending Queue', Icon: Clock },
]

// Only visible to admins / superadmins.
const adminNav = [
  { to: '/users', label: 'Users', Icon: Users },
  { to: '/admin/teams', label: 'Teams', Icon: Users },
  { to: '/admin/users', label: 'All Users', Icon: Users },
  { to: '/admin/auth', label: 'Auth Config', Icon: ShieldCheck },
]

export default function Layout() {
  const { pathname } = useLocation()
  const [role, setRole] = useState<string | null>(null)

  // Load the current user's role once to gate admin-only nav items.
  useEffect(() => {
    let cancelled = false
    getMe()
      .then((m) => { if (!cancelled) setRole(m.role) })
      .catch(() => { /* non-admin / unauth: admin items just stay hidden */ })
    return () => { cancelled = true }
  }, [])

  const isAdmin = role === 'admin' || role === 'superadmin'
  const nav = isAdmin ? [...baseNav, ...adminNav] : baseNav

  const renderItem = ({ to, label, Icon }: { to: string; label: string; Icon: typeof Settings }) => {
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
          {nav.map(renderItem)}
        </List>

        <Divider />

        {/* Settings group (relocated out of the main nav, above the avatar) */}
        <List dense sx={{ py: 1, px: 1 }}>
          {renderItem({ to: '/settings', label: 'Settings', Icon: Settings })}
        </List>

        <Divider />

        {/* User avatar menu (profile, team switcher, links, sign out) */}
        <UserMenu />
      </Drawer>

      {/* Main content */}
      <Box component="main" sx={{ flex: 1, overflow: 'auto', p: 3 }}>
        <Outlet />
      </Box>
    </Box>
  )
}
