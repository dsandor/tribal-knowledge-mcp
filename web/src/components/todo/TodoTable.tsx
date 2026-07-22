import Box from '@mui/material/Box'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'
import type { TodoItem } from '@/lib/api'
import { priorityColor, statusLabel, dueTone } from './todoTheme'

export default function TodoTable({ items, onOpen, onComplete }: {
  items: TodoItem[]
  onOpen: (item: TodoItem) => void
  onComplete: (item: TodoItem) => void
}) {
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell padding="checkbox" />
          <TableCell>Title</TableCell>
          <TableCell>Status</TableCell>
          <TableCell>Priority</TableCell>
          <TableCell>Assignee</TableCell>
          <TableCell>Due</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {items.map(item => {
          const tone = dueTone(item)
          return (
            <TableRow key={item.ID} hover sx={{ cursor: 'pointer' }} onClick={() => onOpen(item)}>
              <TableCell padding="checkbox" onClick={(e) => e.stopPropagation()}>
                <Checkbox
                  size="small"
                  checked={item.Status === 'done'}
                  onChange={() => onComplete(item)}
                />
              </TableCell>
              <TableCell>
                <Typography sx={{ fontSize: 13, textDecoration: item.Status === 'done' ? 'line-through' : 'none' }}>
                  {item.Title}
                </Typography>
              </TableCell>
              <TableCell><Typography sx={{ fontSize: 12 }} color="text.secondary">{statusLabel(item.Status)}</Typography></TableCell>
              <TableCell>
                <Chip size="small" label={item.Priority} sx={{
                  height: 18, fontSize: 10, textTransform: 'uppercase', fontWeight: 600,
                  color: priorityColor(item.Priority), bgcolor: 'transparent',
                  border: `1px solid ${priorityColor(item.Priority)}44`,
                }} />
              </TableCell>
              <TableCell><Typography sx={{ fontSize: 12 }} color="text.secondary">{item.Assignee || '—'}</Typography></TableCell>
              <TableCell>
                <Typography sx={{ fontSize: 12, color: tone === 'overdue' ? '#f87171' : tone === 'soon' ? '#fbbf24' : '#94a3b8' }}>
                  {item.DueDate ? new Date(item.DueDate).toLocaleDateString() : '—'}
                </Typography>
              </TableCell>
            </TableRow>
          )
        })}
        {items.length === 0 && (
          <TableRow>
            <TableCell colSpan={6}>
              <Box sx={{ py: 3, textAlign: 'center' }}>
                <Typography sx={{ fontSize: 13 }} color="text.secondary">No todos match the current filters</Typography>
              </Box>
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  )
}
