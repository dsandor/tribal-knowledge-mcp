import Chip from '@mui/material/Chip'
import Tooltip from '@mui/material/Tooltip'
import AutoAwesome from '@mui/icons-material/AutoAwesome'

interface TagPillProps {
  label: string
  variant: 'user' | 'auto'
  onClick?: () => void
}

// User tags: solid emerald-tinted pill with # prefix.
// Auto tags: outlined indigo pill with a sparkle icon and tooltip.
export function TagPill({ label, variant, onClick }: TagPillProps) {
  if (variant === 'user') {
    return (
      <Chip
        label={`#${label}`}
        size="small"
        onClick={onClick}
        sx={{
          bgcolor: 'rgba(16, 185, 129, 0.18)',
          color: '#34d399',
          fontWeight: 500,
          '&:hover': onClick ? { bgcolor: 'rgba(16, 185, 129, 0.30)' } : undefined,
        }}
      />
    )
  }
  return (
    <Tooltip title="Auto-categorized" arrow>
      <Chip
        label={label}
        size="small"
        variant="outlined"
        onClick={onClick}
        icon={<AutoAwesome sx={{ fontSize: 12, color: '#818cf8 !important' }} />}
        sx={{
          borderColor: 'rgba(99, 102, 241, 0.5)',
          color: '#a5b4fc',
          '&:hover': onClick ? { borderColor: '#6366f1', bgcolor: 'rgba(99, 102, 241, 0.12)' } : undefined,
        }}
      />
    </Tooltip>
  )
}
