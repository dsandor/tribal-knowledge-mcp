import type { TodoItem } from '@/lib/api'

export const STATUS_COLUMNS = [
  { key: 'open', label: 'Open', color: '#94a3b8' },
  { key: 'in_progress', label: 'In Progress', color: '#38bdf8' },
  { key: 'blocked', label: 'Blocked', color: '#f87171' },
  { key: 'done', label: 'Done', color: '#4ade80' },
] as const

export function statusLabel(s: TodoItem['Status']): string {
  const found = STATUS_COLUMNS.find(c => c.key === s)
  return found ? found.label : s === 'cancelled' ? 'Cancelled' : s
}

export const PRIORITY_ORDER = ['urgent', 'high', 'medium', 'low'] as const

export function priorityColor(p: TodoItem['Priority']): string {
  switch (p) {
    case 'urgent': return '#ef4444'
    case 'high': return '#f97316'
    case 'medium': return '#eab308'
    default: return '#64748b'
  }
}

export function providerLabel(p: string): string {
  switch (p) {
    case 'jira': return 'Jira'
    case 'servicenow': return 'ServiceNow'
    case 'github': return 'GitHub'
    case 'gitlab': return 'GitLab'
    default: return 'Link'
  }
}

// dueTone: 'overdue' (red), 'soon' (amber, within 48h), 'normal', or null (no due date / done).
export function dueTone(item: TodoItem): 'overdue' | 'soon' | 'normal' | null {
  if (!item.DueDate || item.Status === 'done' || item.Status === 'cancelled') return null
  const due = new Date(item.DueDate).getTime()
  const now = Date.now()
  if (due < now) return 'overdue'
  if (due < now + 48 * 3600 * 1000) return 'soon'
  return 'normal'
}
