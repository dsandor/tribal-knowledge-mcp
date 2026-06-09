import { useCallback, useEffect, useRef, useState } from 'react'
import GlobalStyles from '@mui/material/GlobalStyles'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import CircularProgress from '@mui/material/CircularProgress'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableHead from '@mui/material/TableHead'
import TableBody from '@mui/material/TableBody'
import TableRow from '@mui/material/TableRow'
import TableCell from '@mui/material/TableCell'
import TableContainer from '@mui/material/TableContainer'
import Alert from '@mui/material/Alert'
import Tooltip from '@mui/material/Tooltip'
import { alpha } from '@mui/material/styles'
import { Activity, Play, CheckCircle, XCircle, Clock, AlertTriangle, Zap, Database, Network } from 'lucide-react'
import { api, fetchPipelineRuns, triggerPipeline } from '../lib/api'
import Counter from '../components/Counter/Counter'

/* ─── Types ──────────────────────────────────────────────────────────────── */
interface PipelineRun {
  ID: string
  Status: string
  Trigger: string
  EntriesProcessed: number
  ClustersFound: number
  Errors: string[] | null
  StartedAt: string
  CompletedAt: string | null
}

interface CurrentStatus {
  status?: string
  Status?: string
  StartedAt?: string
  CompletedAt?: string | null
}

/* ─── Helpers ─────────────────────────────────────────────────────────────── */
function formatDuration(start: string, end: string | null): string {
  if (!end) return '…'
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${s % 60}s`
}

function timeAgo(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function statusColor(status: string): 'success' | 'error' | 'warning' | 'info' | 'default' {
  if (status === 'complete') return 'success'
  if (status === 'failed') return 'error'
  if (status === 'complete_with_errors') return 'warning'
  if (status === 'running') return 'info'
  return 'default'
}

function statusDotColor(status: string): string {
  if (status === 'running') return '#6366f1'
  if (status === 'complete') return '#22c55e'
  if (status === 'complete_with_errors') return '#f59e0b'
  if (status === 'failed') return '#ef4444'
  return '#475569'
}

function statusLabel(status: string): string {
  if (status === 'running') return 'Running'
  if (status === 'complete') return 'Complete'
  if (status === 'complete_with_errors') return 'Complete with Errors'
  if (status === 'failed') return 'Failed'
  return 'Idle'
}

function isRunning(s: string) {
  return s === 'running'
}

/* ─── Animated dot-field canvas background ────────────────────────────────── */
function DotFieldBg({ active }: { active: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rafRef = useRef<number>(0)
  const activeRef = useRef(active)
  activeRef.current = active

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    const dpr = window.devicePixelRatio || 1

    const resize = () => {
      const parent = canvas.parentElement
      if (!parent) return
      const { width, height } = parent.getBoundingClientRect()
      canvas.width = width * dpr
      canvas.height = height * dpr
      canvas.style.width = `${width}px`
      canvas.style.height = `${height}px`
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }
    resize()
    window.addEventListener('resize', resize)

    let frame = 0
    const tick = () => {
      rafRef.current = requestAnimationFrame(tick)
      frame++
      const a = activeRef.current
      const w = canvas.width / dpr
      const h = canvas.height / dpr
      ctx.clearRect(0, 0, w, h)
      const spacing = 22
      const cols = Math.floor(w / spacing)
      const rows = Math.floor(h / spacing)
      const padX = (w % spacing) / 2
      const padY = (h % spacing) / 2
      const t = frame * (a ? 0.035 : 0.008)
      for (let r = 0; r < rows; r++) {
        for (let c = 0; c < cols; c++) {
          const x = padX + c * spacing + spacing / 2
          const y = padY + r * spacing + spacing / 2
          const wave = a
            ? Math.sin(x * 0.045 + t) * Math.cos(y * 0.045 + t * 0.8)
            : Math.sin(x * 0.02 + t) * 0.3
          const opacity = a ? 0.07 + wave * 0.2 : 0.03 + wave * 0.03
          const radius = a ? 1.8 + wave * 0.9 : 1.4
          ctx.beginPath()
          ctx.arc(x, y, Math.max(0.4, radius), 0, Math.PI * 2)
          ctx.fillStyle = a
            ? `rgba(99,102,241,${Math.max(0, opacity)})`
            : `rgba(148,163,184,${Math.max(0, opacity)})`
          ctx.fill()
        }
      }
    }
    tick()
    return () => {
      cancelAnimationFrame(rafRef.current)
      window.removeEventListener('resize', resize)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none' }}
    />
  )
}

/* ─── Pulsing status orb ──────────────────────────────────────────────────── */
function StatusOrb({ status }: { status: string }) {
  const color = statusDotColor(status)
  const running = isRunning(status)
  return (
    <Box sx={{ position: 'relative', display: 'flex', alignItems: 'center', justifyContent: 'center', width: 64, height: 64, flexShrink: 0 }}>
      {running && (
        <>
          <Box sx={{
            position: 'absolute', width: 64, height: 64, borderRadius: '50%',
            background: `radial-gradient(circle, ${alpha(color, 0.35)} 0%, transparent 70%)`,
            animation: 'tkm-orb-pulse 2.2s ease-in-out infinite',
          }} />
          <Box sx={{
            position: 'absolute', width: 44, height: 44, borderRadius: '50%',
            border: `1.5px solid ${alpha(color, 0.45)}`,
            animation: 'tkm-ring-expand 2.2s ease-out infinite',
          }} />
        </>
      )}
      <Box sx={{
        width: 18, height: 18, borderRadius: '50%', bgcolor: color,
        boxShadow: `0 0 14px ${alpha(color, 0.9)}, 0 0 4px ${alpha(color, 0.6)}`,
        zIndex: 1,
        animation: running ? 'tkm-dot-breathe 1.6s ease-in-out infinite' : 'none',
      }} />
    </Box>
  )
}

/* ─── Main page ──────────────────────────────────────────────────────────── */
export default function Pipeline() {
  const [currentStatus, setCurrentStatus] = useState<CurrentStatus | null>(null)
  const [runs, setRuns] = useState<PipelineRun[]>([])
  const [loading, setLoading] = useState(true)
  const [triggering, setTriggering] = useState(false)
  const [triggerError, setTriggerError] = useState<string | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const effectiveStatus = currentStatus?.Status ?? currentStatus?.status ?? 'idle'

  const fetchAll = useCallback(async () => {
    try {
      const [statusRaw, runsData] = await Promise.all([
        api.pipeline.status(),
        fetchPipelineRuns(20),
      ])
      setCurrentStatus(statusRaw as CurrentStatus)
      setRuns(runsData as PipelineRun[])
    } catch {
      // keep stale data on error
    } finally {
      setLoading(false)
    }
  }, [])

  // Adaptive polling: 3s when running, 30s when idle
  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  useEffect(() => {
    if (intervalRef.current) clearInterval(intervalRef.current)
    const delay = isRunning(effectiveStatus) ? 3_000 : 30_000
    intervalRef.current = setInterval(fetchAll, delay)
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [effectiveStatus, fetchAll])

  const handleTrigger = async () => {
    setTriggering(true)
    setTriggerError(null)
    try {
      await triggerPipeline()
      await fetchAll()
    } catch (e) {
      setTriggerError(e instanceof Error ? e.message : 'Trigger failed')
    } finally {
      setTriggering(false)
    }
  }

  const latestCompletedRun = runs.find(r => r.Status !== 'running')

  return (
    <>
      <GlobalStyles styles={`
        @keyframes tkm-orb-pulse {
          0%, 100% { transform: scale(1); opacity: 0.5; }
          50% { transform: scale(1.5); opacity: 0.15; }
        }
        @keyframes tkm-ring-expand {
          0% { transform: scale(0.7); opacity: 0.6; }
          100% { transform: scale(1.7); opacity: 0; }
        }
        @keyframes tkm-dot-breathe {
          0%, 100% { transform: scale(1); }
          50% { transform: scale(1.2); }
        }
      `} />

      <Box sx={{ p: 3, maxWidth: '64rem' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 3 }}>
          <Activity size={20} color="#818cf8" />
          <Typography variant="h5" sx={{ fontWeight: 700 }}>Pipeline Monitor</Typography>
        </Box>

        {/* ── Hero status card ── */}
        <Paper
          elevation={0}
          sx={{
            mb: 3,
            overflow: 'hidden',
            position: 'relative',
            minHeight: 200,
            border: '1px solid',
            borderColor: isRunning(effectiveStatus) ? alpha('#6366f1', 0.5) : 'divider',
            background: isRunning(effectiveStatus)
              ? `linear-gradient(135deg, ${alpha('#1e1b4b', 0.9)}, ${alpha('#0f172a', 0.95)})`
              : alpha('#0f172a', 0.4),
            transition: 'border-color 0.8s ease',
          }}
        >
          <DotFieldBg active={isRunning(effectiveStatus)} />

          <Box sx={{ position: 'relative', zIndex: 1, p: 4 }}>
            {/* Status + trigger row */}
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 3, flexWrap: 'wrap' }}>
              <StatusOrb status={effectiveStatus} />
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="h4" sx={{ fontWeight: 700, letterSpacing: '-0.5px' }}>
                  {statusLabel(effectiveStatus)}
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                  {isRunning(effectiveStatus)
                    ? `Started ${currentStatus?.StartedAt ? timeAgo(currentStatus.StartedAt) : '—'} · refreshing every 3s`
                    : latestCompletedRun
                      ? `Last run ${timeAgo(latestCompletedRun.StartedAt)} · auto-run every hour`
                      : 'No runs yet · auto-run every hour when ≥10 entries exist'}
                </Typography>
              </Box>
              <Tooltip title={isRunning(effectiveStatus) ? 'Pipeline is already running' : 'Trigger an immediate pipeline run'}>
                <span>
                  <Button
                    variant="contained"
                    startIcon={triggering ? <CircularProgress size={13} color="inherit" /> : <Play size={13} />}
                    onClick={handleTrigger}
                    disabled={isRunning(effectiveStatus) || triggering}
                    sx={{
                      background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
                      '&:hover': { background: 'linear-gradient(135deg, #4f46e5, #7c3aed)' },
                      '&.Mui-disabled': { opacity: 0.45, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)' },
                      fontWeight: 600,
                      px: 2.5,
                    }}
                  >
                    {triggering ? 'Triggering…' : 'Run Now'}
                  </Button>
                </span>
              </Tooltip>
            </Box>

            {triggerError && (
              <Alert severity="error" sx={{ mt: 2, maxWidth: 480 }}>{triggerError}</Alert>
            )}

            {/* Animated stat counters */}
            {latestCompletedRun && (
              <Box sx={{ display: 'flex', gap: 5, mt: 3, pt: 3, borderTop: '1px solid', borderColor: alpha('#ffffff', 0.06) }}>
                <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
                  <Box sx={{ mt: 0.5 }}><Database size={15} color="#6366f1" /></Box>
                  <Box>
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, textTransform: 'uppercase', letterSpacing: '0.05em', fontSize: 10 }}>
                      Entries Processed
                    </Typography>
                    <Counter
                      value={latestCompletedRun.EntriesProcessed}
                      fontSize={30}
                      textColor="#e2e8f0"
                      fontWeight={700}
                    />
                  </Box>
                </Box>
                <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
                  <Box sx={{ mt: 0.5 }}><Network size={15} color="#8b5cf6" /></Box>
                  <Box>
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, textTransform: 'uppercase', letterSpacing: '0.05em', fontSize: 10 }}>
                      Clusters Found
                    </Typography>
                    <Counter
                      value={latestCompletedRun.ClustersFound}
                      fontSize={30}
                      textColor="#e2e8f0"
                      fontWeight={700}
                    />
                  </Box>
                </Box>
                <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5 }}>
                  <Box sx={{ mt: 0.5 }}><Clock size={15} color="#64748b" /></Box>
                  <Box>
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5, textTransform: 'uppercase', letterSpacing: '0.05em', fontSize: 10 }}>
                      Duration
                    </Typography>
                    <Typography sx={{ fontSize: 30, fontWeight: 700, color: '#e2e8f0', lineHeight: 1 }}>
                      {formatDuration(latestCompletedRun.StartedAt, latestCompletedRun.CompletedAt)}
                    </Typography>
                  </Box>
                </Box>
              </Box>
            )}
          </Box>
        </Paper>

        {/* ── Run history ── */}
        <Typography variant="h6" sx={{ fontWeight: 600, mb: 1.5 }}>Run History</Typography>

        {loading ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, py: 4 }}>
            <CircularProgress size={18} />
            <Typography color="text.secondary">Loading…</Typography>
          </Box>
        ) : runs.length === 0 ? (
          <Paper elevation={0} sx={{ border: '1px solid', borderColor: 'divider', borderRadius: 2, py: 8 }}>
            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2 }}>
              <Activity size={40} color="#334155" />
              <Typography color="text.secondary" variant="body1" sx={{ fontWeight: 500 }}>No pipeline runs yet</Typography>
              <Typography color="text.secondary" variant="body2">
                Click "Run Now" above, or wait for the automatic hourly trigger.
              </Typography>
            </Box>
          </Paper>
        ) : (
          <TableContainer component={Paper} elevation={0} sx={{ border: '1px solid', borderColor: 'divider' }}>
            <Table size="small">
              <TableHead>
                <TableRow sx={{ '& th': { fontWeight: 600, bgcolor: alpha('#ffffff', 0.03), color: 'text.secondary', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.06em' } }}>
                  <TableCell>Status</TableCell>
                  <TableCell>Trigger</TableCell>
                  <TableCell>Started</TableCell>
                  <TableCell>Duration</TableCell>
                  <TableCell align="right">Entries</TableCell>
                  <TableCell align="right">Clusters</TableCell>
                  <TableCell>Errors</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {runs.map(run => {
                  const hasErrors = run.Errors && run.Errors.length > 0
                  return (
                    <TableRow key={run.ID} hover sx={{ '&:last-child td': { border: 0 } }}>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          {run.Status === 'complete' && <CheckCircle size={13} color="#22c55e" />}
                          {run.Status === 'failed' && <XCircle size={13} color="#ef4444" />}
                          {run.Status === 'complete_with_errors' && <AlertTriangle size={13} color="#f59e0b" />}
                          {run.Status === 'running' && <CircularProgress size={12} sx={{ color: '#6366f1' }} />}
                          <Chip
                            label={statusLabel(run.Status)}
                            size="small"
                            color={statusColor(run.Status)}
                            variant="outlined"
                            sx={{ fontSize: 11, height: 20, '& .MuiChip-label': { px: 1 } }}
                          />
                        </Box>
                      </TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
                          {run.Trigger === 'manual' ? <Zap size={11} color="#8b5cf6" /> : <Clock size={11} color="#64748b" />}
                          <Typography variant="body2" sx={{ fontSize: 12, color: 'text.secondary' }}>
                            {run.Trigger}
                          </Typography>
                        </Box>
                      </TableCell>
                      <TableCell>
                        <Tooltip title={new Date(run.StartedAt).toLocaleString()}>
                          <Typography variant="body2" sx={{ fontSize: 12, color: 'text.secondary', cursor: 'default' }}>
                            {timeAgo(run.StartedAt)}
                          </Typography>
                        </Tooltip>
                      </TableCell>
                      <TableCell>
                        <Typography variant="body2" sx={{ fontSize: 12, fontFamily: 'monospace', color: 'text.secondary' }}>
                          {formatDuration(run.StartedAt, run.CompletedAt)}
                        </Typography>
                      </TableCell>
                      <TableCell align="right">
                        <Typography variant="body2" sx={{ fontSize: 13, fontWeight: 600 }}>
                          {run.EntriesProcessed}
                        </Typography>
                      </TableCell>
                      <TableCell align="right">
                        <Typography variant="body2" sx={{ fontSize: 13, fontWeight: 600, color: run.ClustersFound > 0 ? '#8b5cf6' : 'text.secondary' }}>
                          {run.ClustersFound}
                        </Typography>
                      </TableCell>
                      <TableCell sx={{ maxWidth: 200 }}>
                        {hasErrors ? (
                          <Tooltip title={<Box sx={{ whiteSpace: 'pre-line' }}>{run.Errors!.join('\n')}</Box>}>
                            <Typography variant="body2" sx={{ fontSize: 11, color: '#f59e0b', cursor: 'pointer', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {run.Errors![0]}
                            </Typography>
                          </Tooltip>
                        ) : (
                          <Typography variant="body2" color="text.secondary" sx={{ fontSize: 11 }}>—</Typography>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Box>
    </>
  )
}
