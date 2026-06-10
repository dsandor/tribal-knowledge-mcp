import { useState, useRef, useEffect, useCallback } from 'react'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Collapse from '@mui/material/Collapse'
import Divider from '@mui/material/Divider'
import Fade from '@mui/material/Fade'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import type { ActivityEvent } from '@/lib/api'
import { relativeTime, eventIcon, eventLabel } from '@/lib/activity'

// ─── Constants ────────────────────────────────────────────────────────────────

const MAX_RENDERED = 50

// ─── Helpers ─────────────────────────────────────────────────────────────────

const ACTOR_COLORS = [
  '#a78bfa', '#60a5fa', '#34d399', '#fbbf24', '#f472b6',
  '#fb923c', '#38bdf8', '#c084fc', '#4ade80', '#e879f9',
]

function actorColor(id: string): string {
  let hash = 0
  for (let i = 0; i < id.length; i++) {
    hash = ((hash << 5) - hash) + id.charCodeAt(i)
    hash |= 0
  }
  return ACTOR_COLORS[Math.abs(hash) % ACTOR_COLORS.length]
}

// ─── Fragment row (click-to-expand) ──────────────────────────────────────────

function FragmentText({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false)
  const isTruncatable = text.length > 100

  return (
    <Box
      onClick={isTruncatable ? () => setExpanded(x => !x) : undefined}
      sx={{
        mt: 0.5,
        cursor: isTruncatable ? 'pointer' : 'default',
        bgcolor: 'rgba(255,255,255,0.03)',
        borderLeft: '2px solid rgba(255,255,255,0.1)',
        borderRadius: '0 4px 4px 0',
        px: 1,
        py: 0.25,
      }}
    >
      <Typography
        variant="caption"
        component="p"
        title={isTruncatable && !expanded ? text : undefined}
        sx={{
          color: 'text.secondary',
          fontSize: 10,
          lineHeight: 1.4,
          ...(expanded
            ? {}
            : {
                display: '-webkit-box',
                WebkitBoxOrient: 'vertical',
                WebkitLineClamp: 2,
                overflow: 'hidden',
              }),
        }}
      >
        {text}
      </Typography>
      {isTruncatable && (
        <Typography variant="caption" sx={{ fontSize: 9, color: 'text.disabled', display: 'block', mt: 0.25 }}>
          {expanded ? 'show less' : 'show more'}
        </Typography>
      )}
    </Box>
  )
}

// ─── Single event row ─────────────────────────────────────────────────────────

function EventRow({ event, isNew }: { event: ActivityEvent; isNew: boolean }) {
  const color = actorColor(event.actor.id)

  return (
    <Collapse in timeout={isNew ? 250 : 0} unmountOnExit={false}>
      <Box
        className={isNew ? 'tkm-new-event' : undefined}
        sx={{ py: 0.75, minWidth: 0 }}
      >
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1 }}>
          {/* Icon */}
          <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'center', pt: 0.2 }}>
            {eventIcon(event.type)}
          </Box>

          {/* Main content */}
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
              {/* Actor chip */}
              <Chip
                size="small"
                label={event.actor.display || event.actor.id}
                sx={{
                  height: 18,
                  bgcolor: `${color}18`,
                  border: `1px solid ${color}40`,
                  color,
                  fontWeight: 600,
                  fontSize: 9,
                  '& .MuiChip-label': { px: 0.6 },
                }}
              />
              {/* Label */}
              <Typography
                variant="caption"
                sx={{
                  fontSize: 11,
                  lineHeight: 1.4,
                  color: 'text.primary',
                  minWidth: 0,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  flex: '1 1 auto',
                }}
              >
                {eventLabel(event.type, event.meta)}
              </Typography>
              {/* Time */}
              <Tooltip title={new Date(event.created_at).toLocaleString()} placement="top" arrow>
                <Typography
                  variant="caption"
                  color="text.disabled"
                  sx={{ flexShrink: 0, whiteSpace: 'nowrap', fontSize: 10 }}
                >
                  {relativeTime(event.created_at)}
                </Typography>
              </Tooltip>
            </Box>

            {/* Title (if present and not same as label) */}
            {event.title && (
              <Typography
                variant="caption"
                sx={{
                  display: 'block',
                  mt: 0.25,
                  color: 'text.secondary',
                  fontSize: 10,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {event.title}
              </Typography>
            )}

            {/* Fragment — rendered as plain text (no dangerouslySetInnerHTML) */}
            {event.fragment && <FragmentText text={event.fragment} />}
          </Box>
        </Box>
      </Box>
    </Collapse>
  )
}

// ─── Props ────────────────────────────────────────────────────────────────────

interface LiveActivityFeedProps {
  events: ActivityEvent[]
  connected: boolean
}

// ─── Main component ───────────────────────────────────────────────────────────

export default function LiveActivityFeed({ events, connected }: LiveActivityFeedProps) {
  // Pause-on-hover: buffer incoming events while hovered; apply on mouse-leave
  const [displayedEvents, setDisplayedEvents] = useState<ActivityEvent[]>([])
  const [newIds, setNewIds] = useState<Set<string>>(new Set())
  // isHovered drives the badge re-render; hoveredRef is the sync guard used
  // inside effect callbacks where stale closure state would be wrong.
  const [isHovered, setIsHovered] = useState(false)
  const hoveredRef = useRef(false)
  const pendingRef = useRef<ActivityEvent[]>([])
  const prevEventIdsRef = useRef<Set<string>>(new Set())

  // Apply pending buffer when not hovered
  const applyPending = useCallback(() => {
    if (pendingRef.current.length === 0) return
    const toAdd = pendingRef.current
    pendingRef.current = []
    setDisplayedEvents(prev => {
      const seenIds = new Set(prev.map(e => e.id))
      const fresh = toAdd.filter(e => !seenIds.has(e.id))
      if (fresh.length === 0) return prev
      setNewIds(new Set(fresh.map(e => e.id)))
      setTimeout(() => setNewIds(new Set()), 400)
      return [...fresh, ...prev].slice(0, MAX_RENDERED)
    })
  }, [])

  // Sync incoming events prop to displayed state, respecting hover pause
  useEffect(() => {
    const incoming = events.slice(0, MAX_RENDERED)
    const currentIds = prevEventIdsRef.current

    const freshEvents = incoming.filter(e => !currentIds.has(e.id))
    if (freshEvents.length === 0) return

    // Update tracked set
    const nextIds = new Set(incoming.map(e => e.id))
    prevEventIdsRef.current = nextIds

    if (hoveredRef.current) {
      // Buffer while hovered; cap so a long hover during a burst can't grow unbounded.
      pendingRef.current = [...freshEvents, ...pendingRef.current].slice(0, MAX_RENDERED)
    } else {
      setDisplayedEvents(prev => {
        const seenIds = new Set(prev.map(e => e.id))
        const fresh = freshEvents.filter(e => !seenIds.has(e.id))
        if (fresh.length === 0) return prev
        setNewIds(new Set(fresh.map(e => e.id)))
        setTimeout(() => setNewIds(new Set()), 400)
        return [...fresh, ...prev].slice(0, MAX_RENDERED)
      })
    }
  }, [events])

  const handleMouseEnter = useCallback(() => {
    hoveredRef.current = true
    setIsHovered(true)
  }, [])
  const handleMouseLeave = useCallback(() => {
    hoveredRef.current = false
    setIsHovered(false)
    applyPending()
  }, [applyPending])

  // Empty state
  if (displayedEvents.length === 0) {
    return (
      <Box sx={{ py: 1 }}>
        {!connected && (
          <Fade in timeout={400}>
            <Box
              sx={{
                mb: 1.5, px: 1.5, py: 0.75,
                bgcolor: 'rgba(239,68,68,0.08)',
                border: '1px solid rgba(239,68,68,0.25)',
                borderRadius: 1,
                display: 'flex', alignItems: 'center', gap: 1,
              }}
            >
              <Box
                component="span"
                sx={{
                  width: 6, height: 6, borderRadius: '50%',
                  bgcolor: '#ef4444', flexShrink: 0,
                  animation: 'tkm-pulse-live 1.4s ease-in-out infinite',
                }}
              />
              <Typography variant="caption" sx={{ color: '#ef4444', fontSize: 10 }}>
                Reconnecting to live stream…
              </Typography>
            </Box>
          </Fade>
        )}
        <Typography variant="caption" color="text.secondary">
          No activity yet — knowledge events will appear here.
        </Typography>
      </Box>
    )
  }

  return (
    <Box
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      sx={{ position: 'relative' }}
    >
      {/* Reconnecting banner */}
      {!connected && (
        <Fade in timeout={400}>
          <Box
            sx={{
              mb: 1, px: 1.5, py: 0.5,
              bgcolor: 'rgba(239,68,68,0.08)',
              border: '1px solid rgba(239,68,68,0.25)',
              borderRadius: 1,
              display: 'flex', alignItems: 'center', gap: 1,
            }}
          >
            <Box
              component="span"
              sx={{
                width: 6, height: 6, borderRadius: '50%',
                bgcolor: '#ef4444', flexShrink: 0,
                animation: 'tkm-pulse-live 1.4s ease-in-out infinite',
              }}
            />
            <Typography variant="caption" sx={{ color: '#ef4444', fontSize: 10 }}>
              Reconnecting to live stream…
            </Typography>
          </Box>
        </Fade>
      )}

      {/* Hover-pause hint */}
      {isHovered && pendingRef.current.length > 0 && (
        <Fade in timeout={200}>
          <Box sx={{
            position: 'absolute', top: 0, right: 0,
            bgcolor: 'rgba(167,139,250,0.15)', border: '1px solid rgba(167,139,250,0.35)',
            borderRadius: 1, px: 1, py: 0.25,
            zIndex: 1,
          }}>
            <Typography variant="caption" sx={{ fontSize: 9, color: '#a78bfa' }}>
              {pendingRef.current.length} paused
            </Typography>
          </Box>
        </Fade>
      )}

      <Stack spacing={0} divider={<Divider sx={{ opacity: 0.07 }} />}>
        {displayedEvents.map(ev => (
          <EventRow key={ev.id} event={ev} isNew={newIds.has(ev.id)} />
        ))}
      </Stack>
    </Box>
  )
}
