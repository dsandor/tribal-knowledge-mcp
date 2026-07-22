import { useCallback, useEffect, useMemo, useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import InputAdornment from '@mui/material/InputAdornment'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import Snackbar from '@mui/material/Snackbar'
import { Plus, Search, LayoutGrid, List as ListIcon, ListTodo } from 'lucide-react'
import {
  TodoItem, TodoList, listTodoLists, createTodoList, queryTodos,
  createTodo, updateTodo, completeTodo, reorderTodo,
} from '@/lib/api'
import TodoBoard from '@/components/todo/TodoBoard'
import TodoTable from '@/components/todo/TodoTable'
import TodoDetailDrawer from '@/components/todo/TodoDetailDrawer'

export default function Todos() {
  const [lists, setLists] = useState<TodoList[]>([])
  const [items, setItems] = useState<TodoItem[]>([])
  const [activeList, setActiveList] = useState<string>('all')
  const [view, setView] = useState<'board' | 'list'>('board')
  const [mineOnly, setMineOnly] = useState(false)
  const [priority, setPriority] = useState('')
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [overdueOnly, setOverdueOnly] = useState(false)
  const [quickTitle, setQuickTitle] = useState('')
  const [selected, setSelected] = useState<TodoItem | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const [ls, its] = await Promise.all([
        listTodoLists(),
        queryTodos({
          list_id: activeList === 'all' ? undefined : activeList,
          assignee: mineOnly ? 'me' : undefined,
          priority: priority || undefined,
          q: search || undefined,
          overdue: overdueOnly || undefined,
        }),
      ])
      setLists(ls)
      setItems(its)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed')
    }
  }, [activeList, mineOnly, priority, search, overdueOnly])

  useEffect(() => { load() }, [load])

  const visible = useMemo(() => items.filter(i => i.Status !== 'cancelled'), [items])

  const handleMove = async (id: string, status: TodoItem['Status']) => {
    // Optimistic column move; reconcile with server response.
    setItems(prev => prev.map(i => (i.ID === id ? { ...i, Status: status } : i)))
    try {
      const updated = await updateTodo(id, { Status: status })
      setItems(prev => prev.map(i => (i.ID === id ? updated : i)))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed')
      load()
    }
  }

  // Drop targeted at a specific card: reorder within (or move into) its list,
  // positioning `id` immediately after `afterId` ('' = top of list). Mirrors
  // handleMove's optimistic-update + reconcile/load-on-error pattern.
  const handleReorder = async (id: string, afterId: string, status: TodoItem['Status']) => {
    const dragged = items.find(i => i.ID === id)
    if (!dragged) return
    const statusChanged = dragged.Status !== status

    setItems(prev => {
      const next = prev.map(i => (i.ID === id ? { ...i, Status: status } : i))
      const idx = next.findIndex(i => i.ID === id)
      if (idx === -1) return next
      const [moved] = next.splice(idx, 1)
      if (afterId === '') {
        // Top of list: insert before the first item (any status) in this list.
        const firstIdx = next.findIndex(i => i.ListID === moved.ListID)
        next.splice(firstIdx === -1 ? next.length : firstIdx, 0, moved)
      } else {
        const afterIdx = next.findIndex(i => i.ID === afterId)
        next.splice(afterIdx === -1 ? next.length : afterIdx + 1, 0, moved)
      }
      return next
    })

    try {
      if (statusChanged) await updateTodo(id, { Status: status })
      const updated = await reorderTodo(id, afterId)
      setItems(prev => prev.map(i => (i.ID === updated.ID ? updated : i)))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'reorder failed')
      load()
    }
  }

  const handleComplete = async (item: TodoItem) => {
    try {
      const updated = item.Status === 'done'
        ? await updateTodo(item.ID, { Status: 'open' })
        : await completeTodo(item.ID)
      setItems(prev => prev.map(i => (i.ID === item.ID ? updated : i)))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update failed')
    }
  }

  const handleQuickAdd = async () => {
    const title = quickTitle.trim()
    if (!title) return
    try {
      let listId = activeList
      if (listId === 'all') {
        listId = lists[0]?.ID ?? (await createTodoList({ Name: 'General' })).ID
      }
      const created = await createTodo({ ListID: listId, Title: title })
      setQuickTitle('')
      setItems(prev => [...prev, created])
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed')
    }
  }

  const handleNewList = async () => {
    const name = window.prompt('New list name')
    if (!name?.trim()) return
    try {
      const created = await createTodoList({ Name: name.trim() })
      setLists(prev => [...prev, created])
      setActiveList(created.ID)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create list failed')
    }
  }

  return (
    <Box sx={{ display: 'flex', gap: 2.5, height: '100%' }}>
      {/* Lists rail */}
      <Box sx={{ width: 220, flexShrink: 0 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5 }}>
          <Typography variant="h6" sx={{ fontSize: 16, fontWeight: 600 }}>Todos</Typography>
          <IconButton size="small" onClick={handleNewList} sx={{ ml: 'auto' }} title="New list">
            <Plus size={16} />
          </IconButton>
        </Box>
        {[{ ID: 'all', Name: 'All lists', OpenCount: lists.reduce((n, l) => n + l.OpenCount, 0) } as Pick<TodoList, 'ID' | 'Name' | 'OpenCount'>, ...lists].map(l => (
          <Box
            key={l.ID}
            onClick={() => setActiveList(l.ID)}
            sx={{
              display: 'flex', alignItems: 'center', gap: 1, px: 1.25, py: 0.75, mb: 0.25,
              borderRadius: 1, cursor: 'pointer', fontSize: 13,
              bgcolor: activeList === l.ID ? 'rgba(255,255,255,0.08)' : 'transparent',
              '&:hover': { bgcolor: 'rgba(255,255,255,0.05)' },
            }}
          >
            <ListTodo size={14} color={(l as TodoList).Color || '#94a3b8'} />
            <Typography sx={{ fontSize: 13, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {l.Name}
            </Typography>
            <Typography sx={{ fontSize: 11 }} color="text.secondary">{l.OpenCount}</Typography>
          </Box>
        ))}
      </Box>

      {/* Main area */}
      <Box sx={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        {/* Filter bar */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          <TextField
            size="small" placeholder="Search todos…" value={searchInput}
            onChange={(e) => {
              const v = e.target.value
              setSearchInput(v)
              if (v === '') setSearch('')
            }}
            onKeyDown={(e) => { if (e.key === 'Enter') setSearch(searchInput) }}
            slotProps={{ input: { startAdornment: (
              <InputAdornment position="start"><Search size={14} /></InputAdornment>
            ) } }}
            sx={{ width: 220 }}
          />
          <ToggleButtonGroup size="small" exclusive value={mineOnly ? 'me' : 'all'}
            onChange={(_, v) => v && setMineOnly(v === 'me')}>
            <ToggleButton value="all" sx={{ fontSize: 12, px: 1.5 }}>All</ToggleButton>
            <ToggleButton value="me" sx={{ fontSize: 12, px: 1.5 }}>Mine</ToggleButton>
          </ToggleButtonGroup>
          <TextField select size="small" value={priority} onChange={(e) => setPriority(e.target.value)}
            sx={{ width: 140 }} label="Priority">
            <MenuItem value="">Any</MenuItem>
            <MenuItem value="urgent">Urgent</MenuItem>
            <MenuItem value="high">High</MenuItem>
            <MenuItem value="medium">Medium</MenuItem>
            <MenuItem value="low">Low</MenuItem>
          </TextField>
          <ToggleButton
            size="small" value="overdue" selected={overdueOnly}
            onChange={() => setOverdueOnly(v => !v)}
            sx={{ fontSize: 12, px: 1.5 }}
          >
            Overdue
          </ToggleButton>
          <ToggleButtonGroup size="small" exclusive value={view} onChange={(_, v) => v && setView(v)} sx={{ ml: 'auto' }}>
            <ToggleButton value="board" title="Board view"><LayoutGrid size={14} /></ToggleButton>
            <ToggleButton value="list" title="List view"><ListIcon size={14} /></ToggleButton>
          </ToggleButtonGroup>
        </Box>

        {/* Quick add */}
        <Box sx={{ display: 'flex', gap: 1 }}>
          <TextField
            size="small" fullWidth placeholder="Add a todo… (Enter to save)"
            value={quickTitle}
            onChange={(e) => setQuickTitle(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') handleQuickAdd() }}
          />
          <Button variant="contained" size="small" onClick={handleQuickAdd} startIcon={<Plus size={14} />}>
            Add
          </Button>
        </Box>

        {/* Content */}
        <Box sx={{ flex: 1, overflow: 'auto' }}>
          {view === 'board'
            ? <TodoBoard items={visible} onMove={handleMove} onReorder={handleReorder} onOpen={setSelected} />
            : <TodoTable items={visible} onOpen={setSelected} onComplete={handleComplete} />}
        </Box>
      </Box>

      <TodoDetailDrawer
        item={selected}
        lists={lists}
        onClose={() => setSelected(null)}
        onChanged={(updated) => {
          if (updated) setItems(prev => prev.map(i => (i.ID === updated.ID ? updated : i)))
          else load() // deleted
        }}
      />
      <Snackbar open={!!error} autoHideDuration={4000} onClose={() => setError('')} message={error} />
    </Box>
  )
}
