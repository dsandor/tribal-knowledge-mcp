import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { CalendarClock, Link2, BookOpen, User } from 'lucide-react'
import type { TodoItem } from '@/lib/api'
import { priorityColor, providerLabel, dueTone } from './todoTheme'

export default function TodoCard({
  item, onOpen, draggable = true, dropIndicator = false,
  onCardDragStart, onCardDragOver, onCardDrop, onCardDragEnd,
}: {
  item: TodoItem
  onOpen: (item: TodoItem) => void
  draggable?: boolean
  // When true, render a drop-position indicator above the card (drag-to-reorder hover state).
  dropIndicator?: boolean
  onCardDragStart?: (item: TodoItem) => void
  onCardDragOver?: (e: React.DragEvent, item: TodoItem) => void
  onCardDrop?: (e: React.DragEvent, item: TodoItem) => void
  onCardDragEnd?: () => void
}) {
  const tone = dueTone(item)
  const dueColor = tone === 'overdue' ? '#f87171' : tone === 'soon' ? '#fbbf24' : '#94a3b8'
  const links = item.ExternalLinks ?? []
  const refs = item.KnowledgeRefs ?? []
  return (
    <>
      {dropIndicator && (
        <Box sx={{ height: 2, borderRadius: 1, bgcolor: 'primary.main', mb: 0.5, mx: 0.5 }} />
      )}
      <Box
        draggable={draggable}
        onDragStart={(e) => {
          e.dataTransfer.setData('text/todo-id', item.ID)
          e.dataTransfer.setData('text/todo-list-id', item.ListID)
          onCardDragStart?.(item)
        }}
        onDragEnd={() => onCardDragEnd?.()}
        onDragOver={(e) => {
          // Handle here so the column's own onDragOver (column-level status move)
          // doesn't also fire for a drop targeted at this specific card.
          e.preventDefault()
          e.stopPropagation()
          onCardDragOver?.(e, item)
        }}
        onDrop={(e) => {
          e.preventDefault()
          e.stopPropagation()
          onCardDrop?.(e, item)
        }}
        onClick={() => onOpen(item)}
        sx={{
          p: 1.25, mb: 1, cursor: 'pointer', borderRadius: 1,
          bgcolor: 'background.paper', border: '1px solid', borderColor: 'divider',
          borderLeft: `3px solid ${priorityColor(item.Priority)}`,
          '&:hover': { borderColor: 'primary.main' },
          opacity: item.Status === 'done' || item.Status === 'cancelled' ? 0.6 : 1,
        }}
      >
        <Typography sx={{ fontSize: 13, fontWeight: 500, lineHeight: 1.35 }} color="text.primary">
          {item.Title}
        </Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 0.75, flexWrap: 'wrap' }}>
          <Chip
            size="small"
            label={item.Priority}
            sx={{
              height: 18, fontSize: 10, textTransform: 'uppercase', fontWeight: 600,
              color: priorityColor(item.Priority), bgcolor: 'transparent',
              border: `1px solid ${priorityColor(item.Priority)}44`,
            }}
          />
          {item.DueDate && tone && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: dueColor }}>
              <CalendarClock size={12} />
              <Typography sx={{ fontSize: 11 }}>{new Date(item.DueDate).toLocaleDateString()}</Typography>
            </Box>
          )}
          {item.Assignee && (
            <Tooltip title={item.Assignee}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
                <User size={12} />
                <Typography sx={{ fontSize: 11, maxWidth: 90, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {item.Assignee}
                </Typography>
              </Box>
            </Tooltip>
          )}
          {links.length > 0 && (
            <Tooltip title={links.map(l => `${providerLabel(l.Provider)} ${l.ExternalID}`).join(', ')}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
                <Link2 size={12} />
                <Typography sx={{ fontSize: 11 }}>{links.length}</Typography>
              </Box>
            </Tooltip>
          )}
          {refs.length > 0 && (
            <Tooltip title={`${refs.length} linked knowledge entries`}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: '#94a3b8' }}>
                <BookOpen size={12} />
                <Typography sx={{ fontSize: 11 }}>{refs.length}</Typography>
              </Box>
            </Tooltip>
          )}
        </Box>
      </Box>
    </>
  )
}
