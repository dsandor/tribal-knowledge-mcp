import { useState } from 'react'
import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import type { TodoItem } from '@/lib/api'
import { STATUS_COLUMNS } from './todoTheme'
import TodoCard from './TodoCard'

export default function TodoBoard({ items, onMove, onReorder, onOpen }: {
  items: TodoItem[]
  onMove: (id: string, status: TodoItem['Status']) => void
  // Drop targeted at a specific card: id of the dragged item, afterId of the
  // same-list card it should now follow ('' = top of list), and its new status.
  onReorder: (id: string, afterId: string, status: TodoItem['Status']) => void
  onOpen: (item: TodoItem) => void
}) {
  const [dragOver, setDragOver] = useState<string | null>(null)
  const [dragOverCard, setDragOverCard] = useState<string | null>(null)
  const [draggingId, setDraggingId] = useState<string | null>(null)
  const draggingItem = draggingId ? items.find(i => i.ID === draggingId) ?? null : null

  const resetDragState = () => {
    setDragOver(null)
    setDragOverCard(null)
    setDraggingId(null)
  }

  const handleCardDragOver = (_e: React.DragEvent, hovered: TodoItem) => {
    setDragOver(hovered.Status)
    // Only offer the "insert before this card" indicator when we know what's
    // being dragged, it isn't the hovered card itself, and it belongs to the
    // same list — cross-list hovers (possible in the "All lists" view) fall
    // back to a plain column drop (status move only, no indicator).
    if (draggingItem && draggingItem.ID !== hovered.ID && draggingItem.ListID === hovered.ListID) {
      setDragOverCard(hovered.ID)
    } else {
      setDragOverCard(null)
    }
  }

  const handleCardDrop = (e: React.DragEvent, hovered: TodoItem) => {
    const id = e.dataTransfer.getData('text/todo-id')
    resetDragState()
    // Self-drop guard: dropping a card onto itself would send AfterID ==
    // TodoID, which the backend treats as a move-to-top — never trigger it.
    if (!id || id === hovered.ID) return
    const dragged = items.find(i => i.ID === id)
    if (!dragged) return

    if (dragged.ListID !== hovered.ListID) {
      // Cross-list card hover: no valid same-list AfterID to compute, so this
      // degrades to a plain column drop (status move only).
      if (dragged.Status !== hovered.Status) onMove(id, hovered.Status)
      return
    }

    // Insert before the hovered card: AfterID is the nearest preceding card in
    // this column's rendered order that belongs to the same list ('' = top).
    const colItems = items.filter(i => i.Status === hovered.Status)
    const hoveredIdx = colItems.findIndex(i => i.ID === hovered.ID)
    let afterId = ''
    for (let j = hoveredIdx - 1; j >= 0; j--) {
      if (colItems[j].ID === dragged.ID) continue
      if (colItems[j].ListID === dragged.ListID) {
        afterId = colItems[j].ID
        break
      }
    }
    onReorder(id, afterId, hovered.Status)
  }

  return (
    <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-start', overflowX: 'auto', pb: 1 }}>
      {STATUS_COLUMNS.map(col => {
        const colItems = items.filter(i => i.Status === col.key)
        return (
          <Box
            key={col.key}
            onDragOver={(e) => { e.preventDefault(); setDragOver(col.key); setDragOverCard(null) }}
            onDragLeave={() => { setDragOver(null); setDragOverCard(null) }}
            onDrop={(e) => {
              e.preventDefault()
              const id = e.dataTransfer.getData('text/todo-id')
              resetDragState()
              const found = items.find(i => i.ID === id)
              if (found && found.Status !== col.key) onMove(id, col.key as TodoItem['Status'])
            }}
            sx={{
              flex: '1 1 0', minWidth: 240, borderRadius: 1.5, p: 1.25,
              bgcolor: dragOver === col.key ? 'rgba(255,255,255,0.06)' : 'rgba(255,255,255,0.02)',
              border: '1px solid', borderColor: dragOver === col.key ? col.color : 'divider',
              transition: 'border-color 120ms, background-color 120ms',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1.25, px: 0.5 }}>
              <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: col.color }} />
              <Typography sx={{ fontSize: 12, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5 }} color="text.secondary">
                {col.label}
              </Typography>
              <Typography sx={{ fontSize: 12, ml: 'auto' }} color="text.secondary">{colItems.length}</Typography>
            </Box>
            {colItems.map(item => (
              <TodoCard
                key={item.ID}
                item={item}
                onOpen={onOpen}
                dropIndicator={dragOverCard === item.ID}
                onCardDragStart={(dragged) => setDraggingId(dragged.ID)}
                onCardDragOver={handleCardDragOver}
                onCardDrop={handleCardDrop}
                onCardDragEnd={resetDragState}
              />
            ))}
            {colItems.length === 0 && (
              <Typography sx={{ fontSize: 12, textAlign: 'center', py: 2 }} color="text.secondary">
                Drop items here
              </Typography>
            )}
          </Box>
        )
      })}
    </Box>
  )
}
