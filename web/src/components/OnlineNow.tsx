import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Grow from '@mui/material/Grow'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import type { ActorRef } from '@/lib/api'

// ─── Props ────────────────────────────────────────────────────────────────────

interface OnlineNowProps {
  online: ActorRef[]
  count?: number
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function actorInitials(display: string): string {
  const parts = display.trim().split(/\s+/)
  if (parts.length >= 2) {
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
  }
  return display.slice(0, 2).toUpperCase()
}

// Stable color derived from actor id so it doesn't change across renders
const AVATAR_COLORS = [
  '#a78bfa', '#60a5fa', '#34d399', '#fbbf24', '#f472b6',
  '#fb923c', '#38bdf8', '#c084fc', '#4ade80', '#e879f9',
]

function avatarColor(id: string): string {
  let hash = 0
  for (let i = 0; i < id.length; i++) {
    hash = ((hash << 5) - hash) + id.charCodeAt(i)
    hash |= 0
  }
  return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length]
}

// ─── Component ────────────────────────────────────────────────────────────────

export default function OnlineNow({ online, count }: OnlineNowProps) {
  // If server reports a higher count than our client roster, use server count
  const displayCount = count !== undefined ? Math.max(count, online.length) : online.length

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', minHeight: 28 }}>
      {/* Online indicator + count */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
        <Box
          component="span"
          sx={{
            display: 'inline-block',
            width: 7,
            height: 7,
            borderRadius: '50%',
            bgcolor: '#22c55e',
            animation: 'tkm-pulse-live 1.8s ease-in-out infinite',
            flexShrink: 0,
          }}
        />
        <Typography
          variant="caption"
          sx={{ color: '#22c55e', fontWeight: 600, fontSize: 11 }}
        >
          {displayCount} online
        </Typography>
      </Box>

      {/* Actor chips — animate in with Grow */}
      {online.map(actor => {
        const color = avatarColor(actor.id)
        const initials = actorInitials(actor.display)
        return (
          <Grow key={actor.id} in timeout={250}>
            <Tooltip title={actor.display} placement="top" arrow>
              <Chip
                size="small"
                label={
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                    {/* Mini avatar circle */}
                    <Box
                      component="span"
                      sx={{
                        width: 16,
                        height: 16,
                        borderRadius: '50%',
                        bgcolor: `${color}30`,
                        border: `1px solid ${color}60`,
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontSize: 8,
                        fontWeight: 700,
                        color,
                        flexShrink: 0,
                      }}
                    >
                      {initials}
                    </Box>
                    <Typography
                      component="span"
                      sx={{
                        fontSize: 10,
                        fontWeight: 500,
                        color: 'text.primary',
                        maxWidth: 72,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {actor.display}
                    </Typography>
                  </Box>
                }
                sx={{
                  height: 22,
                  bgcolor: `${color}12`,
                  border: `1px solid ${color}35`,
                  '& .MuiChip-label': { px: 0.75 },
                  cursor: 'default',
                }}
              />
            </Tooltip>
          </Grow>
        )
      })}

      {/* If server count exceeds known actors, show overflow badge */}
      {count !== undefined && count > online.length && (
        <Grow in timeout={200}>
          <Chip
            size="small"
            label={`+${count - online.length} more`}
            sx={{
              height: 22,
              bgcolor: 'rgba(255,255,255,0.06)',
              border: '1px solid rgba(255,255,255,0.12)',
              '& .MuiChip-label': { px: 0.75, fontSize: 10 },
              color: 'text.secondary',
            }}
          />
        </Grow>
      )}
    </Box>
  )
}
