import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom'
import Box from '@mui/material/Box'
import CircularProgress from '@mui/material/CircularProgress'
import { checkAuth } from '@/lib/api'
import Layout from '@/components/Layout'
import Dashboard from '@/pages/Dashboard'
import KnowledgeBrowser from '@/pages/KnowledgeBrowser'
import KnowledgeDetail from '@/pages/KnowledgeDetail'
import Clusters from '@/pages/Clusters'
import Datasets from '@/pages/Datasets'
import Agents from '@/pages/Agents'
import AgentDetail from '@/pages/AgentDetail'
import Analytics from './pages/Analytics'
import PendingQueue from './pages/PendingQueue'
import Settings from './pages/Settings'
import AdminTeams from './pages/AdminTeams'
import AdminUsers from './pages/AdminUsers'
import AuthConfig from './pages/AuthConfig'
import Onboarding from './pages/Onboarding'
import Import from './pages/Import'
import Pipeline from './pages/Pipeline'
import APIKeys from './pages/APIKeys'
import Users from './pages/Users'
import MyVisibility from './pages/MyVisibility'
import Enrichment from './pages/Enrichment'
import ShareLanding from './pages/ShareLanding'
import Login from './pages/Login'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  // Verify against the server so both Bearer-key (superadmin) and OIDC
  // session-cookie logins are recognized. null = checking.
  const [authed, setAuthed] = useState<boolean | null>(null)

  useEffect(() => {
    let active = true
    checkAuth()
      .then(ok => { if (active) setAuthed(ok) })
      .catch(() => { if (active) setAuthed(false) })
    return () => { active = false }
  }, [])

  if (authed === null) {
    return (
      <Box sx={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress size={28} />
      </Box>
    )
  }
  if (!authed) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />
  }
  return <>{children}</>
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Public routes */}
        <Route path="/login" element={<Login />} />

        {/* Full-screen authenticated routes (no Layout sidebar) */}
        <Route
          path="/onboarding"
          element={<RequireAuth><Onboarding /></RequireAuth>}
        />

        <Route
          path="/"
          element={<RequireAuth><Layout /></RequireAuth>}
        >
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<Dashboard />} />
          <Route path="knowledge" element={<KnowledgeBrowser />} />
          <Route path="knowledge/:id" element={<KnowledgeDetail />} />
          <Route path="clusters" element={<Clusters />} />
          <Route path="datasets" element={<Datasets />} />
          <Route path="agents" element={<Agents />} />
          <Route path="agents/:id" element={<AgentDetail />} />
          <Route path="analytics" element={<Analytics />} />
          <Route path="pending" element={<PendingQueue />} />
          <Route path="settings" element={<Settings />} />
          <Route path="admin/teams" element={<AdminTeams />} />
          <Route path="admin/users" element={<AdminUsers />} />
          <Route path="admin/auth" element={<AuthConfig />} />
          <Route path="import" element={<Import />} />
          <Route path="pipeline" element={<Pipeline />} />
          <Route path="api-keys" element={<APIKeys />} />
          <Route path="users" element={<Users />} />
          <Route path="my-visibility" element={<MyVisibility />} />
          <Route path="enrichment" element={<Enrichment />} />
          <Route path="share/:token" element={<ShareLanding />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
