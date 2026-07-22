import { useEffect, useRef, useState, useCallback } from 'react'
import {
  api, fetchTrending, fetchUsage, fetchContributions, getOnboardingStatus, queryTodos,
  type Stats, type TrendingEntry, type TodoItem,
} from '@/lib/api'
import { priorityColor, dueTone } from '@/components/todo/todoTheme'
import {
  BookOpen, Network, Bot, Zap, Users, TrendingUp,
} from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import GlobalStyles from '@mui/material/GlobalStyles'
import Grid from '@mui/material/Grid'
import Skeleton from '@mui/material/Skeleton'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import Counter from '@/components/Counter/Counter'
import OnlineNow from '@/components/OnlineNow'
import LiveActivityFeed from '@/components/LiveActivityFeed'
import { useActivityStream } from '@/hooks/useActivityStream'

// ─── Types ──────────────────────────────────────────────────────────────────

interface UsageData {
  top_entries: { id: string; title: string; domain: string; score: number; usage_count: number; rating: number }[]
  by_domain:   { domain: string; entry_count: number; avg_rating: number; total_usage: number }[]
  heatmap:     { week: string; domain: string; usage: number }[]
}

interface ContributionsData {
  leaderboard: { author: string; entry_count: number; approved_count: number; total_usage: number; avg_rating: number; score: number }[]
}

// ─── Global animations (injected once) ───────────────────────────────────────

const GLOBAL_STYLES = (
  <GlobalStyles styles={`
    @keyframes tkm-slide-in {
      from { opacity: 0; transform: translateY(-6px); }
      to   { opacity: 1; transform: translateY(0); }
    }
    @keyframes tkm-pulse-live {
      0%, 100% { opacity: 1; transform: scale(1); }
      50%       { opacity: 0.4; transform: scale(0.8); }
    }
    .tkm-new-event {
      animation: tkm-slide-in 0.25s ease both;
    }
    .tkm-bar-fill {
      transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1);
    }
  `} />
)

// ─── LiveDot ──────────────────────────────────────────────────────────────────

function LiveDot() {
  return (
    <Box component="span" sx={{
      display: 'inline-block', width: 7, height: 7, borderRadius: '50%',
      bgcolor: '#22c55e', animation: 'tkm-pulse-live 1.8s ease-in-out infinite',
      flexShrink: 0,
    }} />
  )
}

// ─── Stat card with animated counter ─────────────────────────────────────────

function StatCard({
  title, value, icon: Icon, color, subtitle, to,
}: {
  title: string
  value: number
  icon: React.ElementType
  color: string
  subtitle?: string
  to?: string
}) {
  return (
    <Card
      {...(to ? { component: Link, to } : {})}
      sx={{
        height: '100%',
        display: 'block',
        textDecoration: 'none',
        ...(to && {
          cursor: 'pointer',
          transition: 'transform 0.15s ease, border-color 0.15s ease',
          '&:hover': {
            transform: 'translateY(-2px)',
            borderColor: color,
          },
        }),
      }}
    >
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', mb: 1 }}>
          <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 500, letterSpacing: '0.04em', textTransform: 'uppercase', fontSize: 10 }}>
            {title}
          </Typography>
          <Box sx={{
            width: 28, height: 28, borderRadius: 1.5,
            bgcolor: `${color}18`, display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            <Icon style={{ width: 14, height: 14, color }} />
          </Box>
        </Box>
        <Counter value={value} fontSize={28} fontWeight={700} />
        {subtitle && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
            {subtitle}
          </Typography>
        )}
      </CardContent>
    </Card>
  )
}

// ─── Trending entries ─────────────────────────────────────────────────────────

function TrendingPanel({ entries }: { entries: TrendingEntry[] | null }) {
  if (entries === null) {
    return (
      <Stack spacing={1}>
        {[0, 1, 2, 3, 4].map(i => (
          <Box key={i} sx={{ display: 'flex', gap: 1 }}>
            <Skeleton variant="text" sx={{ flex: 1 }} />
            <Skeleton variant="text" width={50} />
          </Box>
        ))}
      </Stack>
    )
  }
  if (entries.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary">
        No trending data yet — start using <code>knowledge_search</code> to see trends.
      </Typography>
    )
  }
  const max = Math.max(...entries.map(e => e.signal_score), 0.1)
  return (
    <Stack spacing={1.5}>
      {entries.map(e => (
        <Box key={e.ID}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.4 }}>
            <Typography
              component={Link}
              to={`/knowledge/${e.ID}`}
              variant="caption"
              sx={{
                flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
                whiteSpace: 'nowrap', color: 'text.primary', textDecoration: 'none',
                fontWeight: 500, '&:hover': { color: 'primary.light' },
              }}
            >
              {e.Title}
            </Typography>
            <Typography variant="caption" sx={{ color: '#fbbf24', flexShrink: 0, fontWeight: 600 }}>
              ⚡ {e.signal_score.toFixed(1)}
            </Typography>
            <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0, whiteSpace: 'nowrap' }}>
              ↑ {e.usage_count_7d}
            </Typography>
          </Box>
          <Box sx={{ height: 3, bgcolor: 'rgba(255,255,255,0.06)', borderRadius: 4, overflow: 'hidden' }}>
            <Box
              className="tkm-bar-fill"
              sx={{
                height: '100%',
                width: `${(e.signal_score / max) * 100}%`,
                bgcolor: '#fbbf24',
                borderRadius: 4,
                opacity: 0.7,
              }}
            />
          </Box>
        </Box>
      ))}
    </Stack>
  )
}

// ─── Top entries by usage ─────────────────────────────────────────────────────

function TopEntriesPanel({ data }: { data: UsageData | null }) {
  if (data === null) {
    return (
      <Stack spacing={1}>
        {[0, 1, 2, 3].map(i => <Skeleton key={i} variant="rectangular" height={32} sx={{ borderRadius: 1 }} />)}
      </Stack>
    )
  }
  const entries = data.top_entries.slice(0, 8)
  if (entries.length === 0) {
    return <Typography variant="caption" color="text.secondary">No usage recorded yet.</Typography>
  }
  const max = Math.max(...entries.map(e => e.usage_count), 1)
  return (
    <Stack spacing={1.5}>
      {entries.map(e => (
        <Box key={e.id}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.4 }}>
            <Typography
              variant="caption"
              sx={{
                flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
                whiteSpace: 'nowrap', fontWeight: 500,
              }}
            >
              {e.title}
            </Typography>
            <Chip label={e.domain || 'general'} size="small" sx={{ height: 16, fontSize: 9 }} />
            <Typography variant="caption" sx={{ color: '#60a5fa', flexShrink: 0, fontWeight: 600 }}>
              {e.usage_count}×
            </Typography>
          </Box>
          <Box sx={{ height: 3, bgcolor: 'rgba(255,255,255,0.06)', borderRadius: 4, overflow: 'hidden' }}>
            <Box
              className="tkm-bar-fill"
              sx={{
                height: '100%',
                width: `${(e.usage_count / max) * 100}%`,
                bgcolor: '#60a5fa',
                borderRadius: 4,
              }}
            />
          </Box>
        </Box>
      ))}
    </Stack>
  )
}

// ─── Domain breakdown ─────────────────────────────────────────────────────────

function DomainPanel({ data }: { data: UsageData | null }) {
  if (data === null) {
    return (
      <Stack spacing={1}>
        {[0, 1, 2].map(i => <Skeleton key={i} variant="rectangular" height={36} sx={{ borderRadius: 1 }} />)}
      </Stack>
    )
  }
  const domains = data.by_domain.slice(0, 8)
  if (domains.length === 0) {
    return <Typography variant="caption" color="text.secondary">No domain data yet.</Typography>
  }
  const maxUsage = Math.max(...domains.map(d => d.total_usage), 1)
  return (
    <Stack spacing={1.5}>
      {domains.map(d => (
        <Box key={d.domain}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.4 }}>
            <Typography variant="caption" sx={{ flex: 1, fontWeight: 500, textTransform: 'capitalize' }}>
              {d.domain || 'general'}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {d.entry_count} entries
            </Typography>
            <Typography variant="caption" sx={{ color: '#a78bfa', fontWeight: 600 }}>
              {d.total_usage} uses
            </Typography>
            <Typography variant="caption" color="text.secondary">
              ★ {d.avg_rating.toFixed(1)}
            </Typography>
          </Box>
          <Box sx={{ height: 4, bgcolor: 'rgba(255,255,255,0.06)', borderRadius: 4, overflow: 'hidden' }}>
            <Box
              className="tkm-bar-fill"
              sx={{
                height: '100%',
                width: `${(d.total_usage / maxUsage) * 100}%`,
                bgcolor: '#a78bfa',
                borderRadius: 4,
                opacity: 0.8,
              }}
            />
          </Box>
        </Box>
      ))}
    </Stack>
  )
}

// ─── Contributors ─────────────────────────────────────────────────────────────

function ContributorsPanel({ data }: { data: ContributionsData | null }) {
  if (data === null) {
    return (
      <Stack spacing={0.5}>
        {[0, 1, 2].map(i => <Skeleton key={i} variant="rectangular" height={40} sx={{ borderRadius: 1 }} />)}
      </Stack>
    )
  }
  const list = data.leaderboard.slice(0, 5)
  if (list.length === 0) {
    return <Typography variant="caption" color="text.secondary">No contribution data yet.</Typography>
  }
  const medals = ['🥇', '🥈', '🥉']
  return (
    <Stack spacing={0} divider={<Box sx={{ borderBottom: '1px solid rgba(255,255,255,0.08)' }} />}>
      {list.map((c, i) => (
        <Box key={c.author} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, py: 0.9 }}>
          <Typography sx={{ fontSize: 14, flexShrink: 0, width: 20, textAlign: 'center' }}>
            {medals[i] ?? <Typography variant="caption" color="text.secondary">{i + 1}</Typography>}
          </Typography>
          <Typography variant="caption" sx={{ flex: 1, fontWeight: 600, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {c.author || 'anonymous'}
          </Typography>
          <Tooltip title={`${c.approved_count} approved · ★ ${c.avg_rating.toFixed(1)} · ${c.total_usage} uses`} placement="top">
            <Box sx={{ display: 'flex', gap: 1, flexShrink: 0 }}>
              <Typography variant="caption" color="text.secondary">{c.entry_count} entries</Typography>
              <Typography variant="caption" sx={{ color: '#fbbf24', fontWeight: 600 }}>
                {c.score.toFixed(0)} pts
              </Typography>
            </Box>
          </Tooltip>
        </Box>
      ))}
    </Stack>
  )
}

// ─── Main Dashboard ───────────────────────────────────────────────────────────

export default function Dashboard() {
  const navigate = useNavigate()

  // ── SSE live stream (replaces 6s activity poll) ──
  const { events, online, onlineCount, connected } = useActivityStream()

  // ── State buckets ──
  const [stats,       setStats]       = useState<Stats | null>(null)
  const [trending,    setTrending]     = useState<TrendingEntry[] | null>(null)
  const [usageData,   setUsageData]    = useState<UsageData | null>(null)
  const [contribData, setContribData]  = useState<ContributionsData | null>(null)
  const [myTodos,     setMyTodos]      = useState<TodoItem[]>([])
  const [error,       setError]        = useState<string | null>(null)
  const [loaded,      setLoaded]       = useState(false)

  const overdueCount = myTodos.filter(t => dueTone(t) === 'overdue').length

  // Extra counters derived from analytics
  const totalUsage = usageData ? usageData.by_domain.reduce((s, d) => s + d.total_usage, 0) : 0

  // ── Fetch helpers ──

  const fetchStats = useCallback(async () => {
    try {
      const s = await api.stats()
      setStats(s)
    } catch { /* keep previous value */ }
  }, [])

  const fetchTrendingData = useCallback(async () => {
    try {
      const data = await fetchTrending(7, 7)
      setTrending(data)
    } catch { /* keep previous */ }
  }, [])

  const fetchAnalytics = useCallback(async () => {
    try {
      const [u, c] = await Promise.all([fetchUsage(), fetchContributions()])
      setUsageData(u)
      setContribData(c)
    } catch { /* keep previous */ }
  }, [])

  // ── Initial load ──
  // Use a ref to avoid double-invoke in StrictMode messing up the onboarding check
  const initialLoadDoneRef = useRef(false)
  useEffect(() => {
    if (initialLoadDoneRef.current) return
    initialLoadDoneRef.current = true
    // Authoritative onboarding check: only a superadmin on a deployment with no
    // real team yet should be routed to onboarding. Swallow errors.
    getOnboardingStatus()
      .then(({ needs_onboarding }) => {
        if (needs_onboarding) navigate('/onboarding', { replace: true })
      })
      .catch(() => { /* never trap the user on failure */ })
    Promise.all([
      fetchStats(),
      fetchTrendingData(),
      fetchAnalytics(),
    ]).catch(() => setError('Failed to load dashboard')).finally(() => setLoaded(true))
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Stats: fast poll (6s) ──
  useEffect(() => {
    const id = setInterval(fetchStats, 6_000)
    return () => clearInterval(id)
  }, [fetchStats])

  // ── Trending: medium poll (15s) ──
  useEffect(() => {
    const id = setInterval(fetchTrendingData, 15_000)
    return () => clearInterval(id)
  }, [fetchTrendingData])

  // ── Analytics: slow poll (30s) ──
  useEffect(() => {
    const id = setInterval(fetchAnalytics, 30_000)
    return () => clearInterval(id)
  }, [fetchAnalytics])

  // ── My Todos: one-shot load for the dashboard widget ──
  useEffect(() => {
    queryTodos({ assignee: 'me', status: 'open,in_progress,blocked' })
      .then(setMyTodos)
      .catch(() => setMyTodos([]))
  }, [])

  if (error && !loaded) return <Alert severity="error">Error: {error}</Alert>
  if (!loaded) return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, pt: 4 }}>
      <CircularProgress size={20} />
      <Typography color="text.secondary">Loading…</Typography>
    </Box>
  )

  return (
    <>
      {GLOBAL_STYLES}
      <Stack spacing={3}>

        {/* ── Title row ── */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Typography variant="h6" sx={{ fontWeight: 600 }}>Dashboard</Typography>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <LiveDot />
            <Typography variant="caption" color="text.secondary">live</Typography>
          </Box>
        </Box>

        {/* ── Hero stats row ── */}
        <Grid container spacing={2}>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard title="Entries" value={stats?.knowledge_count ?? 0} icon={BookOpen} color="#60a5fa" to="/knowledge" />
          </Grid>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard title="Clusters" value={stats?.cluster_count ?? 0} icon={Network} color="#a78bfa" to="/clusters" />
          </Grid>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard title="Agents" value={stats?.agent_count ?? 0} icon={Bot} color="#34d399" to="/agents" />
          </Grid>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard title="Total Uses" value={totalUsage} icon={Zap} color="#fbbf24"
              subtitle={stats?.pipeline_status ? `Pipeline: ${stats.pipeline_status}` : undefined}
              to="/analytics"
            />
          </Grid>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard
              title="Online Now"
              value={Math.max(online.length, onlineCount)}
              icon={Users}
              color="#f472b6"
            />
          </Grid>
          <Grid size={{ xs: 6, sm: 4, lg: 2 }}>
            <StatCard title="Trending" value={trending?.length ?? 0} icon={TrendingUp} color="#fb923c"
              subtitle={trending && trending.length > 0 ? `Top: ${trending[0].signal_score.toFixed(1)} signal` : undefined}
              to="/analytics"
            />
          </Grid>
        </Grid>

        {/* ── Middle row: activity + trending ── */}
        <Grid container spacing={2}>
          {/* Live activity feed */}
          <Grid size={{ xs: 12, lg: 5 }}>
            <Card sx={{ height: '100%', minHeight: 320 }}>
              <CardHeader
                title={
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Typography variant="subtitle2">Team Activity</Typography>
                      <LiveDot />
                    </Box>
                    {/* Online-now widget inline in header */}
                    <OnlineNow online={online} count={onlineCount > 0 ? onlineCount : undefined} />
                  </Box>
                }
                subheader={
                  <Typography variant="caption" color="text.secondary">
                    {connected ? 'Live stream — events from all users' : 'Reconnecting to live stream…'}
                  </Typography>
                }
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0, maxHeight: 360, overflowY: 'auto' }}>
                <LiveActivityFeed events={events} connected={connected} />
              </CardContent>
            </Card>
          </Grid>

          {/* Trending this week */}
          <Grid size={{ xs: 12, lg: 4 }}>
            <Card sx={{ height: '100%', minHeight: 320 }}>
              <CardHeader
                title={<Typography variant="subtitle2">Trending This Week</Typography>}
                subheader={
                  <Typography variant="caption" color="text.secondary">
                    By signal score (usage + ratings)
                  </Typography>
                }
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0 }}>
                <TrendingPanel entries={trending} />
              </CardContent>
            </Card>
          </Grid>

          {/* Top entries by raw usage count */}
          <Grid size={{ xs: 12, lg: 3 }}>
            <Card sx={{ height: '100%', minHeight: 320 }}>
              <CardHeader
                title={<Typography variant="subtitle2">Most Used</Typography>}
                subheader={<Typography variant="caption" color="text.secondary">By usage count</Typography>}
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0 }}>
                <TopEntriesPanel data={usageData} />
              </CardContent>
            </Card>
          </Grid>
        </Grid>

        {/* ── Bottom row: domain breakdown + contributors ── */}
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 8 }}>
            <Card>
              <CardHeader
                title={<Typography variant="subtitle2">Usage by Domain</Typography>}
                subheader={<Typography variant="caption" color="text.secondary">Entries, total uses, avg rating per domain</Typography>}
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0 }}>
                <DomainPanel data={usageData} />
              </CardContent>
            </Card>
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <Card>
              <CardHeader
                title={<Typography variant="subtitle2">Top Contributors</Typography>}
                subheader={<Typography variant="caption" color="text.secondary">Ranked by contribution score</Typography>}
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0 }}>
                <ContributorsPanel data={contribData} />
              </CardContent>
            </Card>
          </Grid>
        </Grid>

        {/* ── My Todos widget ── */}
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 4 }}>
            <Card>
              <CardHeader
                title={<Typography variant="subtitle2">My Todos</Typography>}
                subheader={<Typography variant="caption" color="text.secondary">Open items assigned to you</Typography>}
                sx={{ pb: 1 }}
              />
              <CardContent sx={{ pt: 0 }}>
                <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 1, mb: 1 }}>
                  <Typography sx={{ fontSize: 24, fontWeight: 700 }}>{myTodos.length}</Typography>
                  <Typography sx={{ fontSize: 12 }} color="text.secondary">open</Typography>
                  {overdueCount > 0 && (
                    <Typography sx={{ fontSize: 12, color: '#f87171', ml: 1 }}>{overdueCount} overdue</Typography>
                  )}
                </Box>
                {myTodos.slice(0, 5).map(t => (
                  <Box key={t.ID} component={Link} to="/todos" sx={{
                    display: 'flex', alignItems: 'center', gap: 1, py: 0.5,
                    textDecoration: 'none', color: 'inherit', '&:hover': { opacity: 0.8 },
                  }}>
                    <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: priorityColor(t.Priority), flexShrink: 0 }} />
                    <Typography sx={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {t.Title}
                    </Typography>
                  </Box>
                ))}
                {myTodos.length === 0 && (
                  <Typography sx={{ fontSize: 12 }} color="text.secondary">Nothing assigned to you 🎉</Typography>
                )}
              </CardContent>
            </Card>
          </Grid>
        </Grid>

        {/* ── Pipeline footnote ── */}
        {stats?.pipeline_last_run && (
          <Typography variant="caption" color="text.secondary" sx={{ opacity: 0.6 }}>
            Last pipeline run: {new Date(stats.pipeline_last_run).toLocaleString()}
          </Typography>
        )}

      </Stack>
    </>
  )
}
