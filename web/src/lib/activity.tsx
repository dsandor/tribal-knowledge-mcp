import type { ReactElement } from 'react'
import {
  BookOpen, Star, CheckCircle, XCircle, GitMerge, Sparkles, Bot, Activity,
  Zap, ThumbsUp, LogIn, ListTodo, CheckSquare,
} from 'lucide-react'
import type { ActivityEvent } from './api'

// ─── relativeTime ─────────────────────────────────────────────────────────────

export function relativeTime(iso: string): string {
  const sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (sec < 60)    return 'just now'
  if (sec < 3600)  return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

// ─── eventIcon ────────────────────────────────────────────────────────────────

type IconElement = ReactElement

const ICON_SIZE = { width: 12, height: 12 }

export function eventIcon(type: string): IconElement {
  const s = ICON_SIZE
  switch (type) {
    case 'knowledge_stored':       return <BookOpen style={{ ...s, color: '#22c55e' }} />
    case 'knowledge_used':         return <Zap style={{ ...s, color: '#fbbf24' }} />
    case 'knowledge_rated':        return <Star style={{ ...s, color: '#facc15' }} />
    case 'enrich_context':         return <Sparkles style={{ ...s, color: '#a78bfa' }} />
    case 'approved':               return <CheckCircle style={{ ...s, color: '#059669' }} />
    case 'rejected':               return <XCircle style={{ ...s, color: '#dc2626' }} />
    case 'pipeline_complete':      return <GitMerge style={{ ...s, color: '#60a5fa' }} />
    case 'improvement_drafted':    return <ThumbsUp style={{ ...s, color: '#a78bfa' }} />
    case 'agent_generated':        return <Bot style={{ ...s, color: '#34d399' }} />
    case 'signin':                 return <LogIn style={{ ...s, color: '#94a3b8' }} />
    case 'todo_created':           return <ListTodo style={{ ...s, color: '#38bdf8' }} />
    case 'todo_completed':         return <CheckSquare style={{ ...s, color: '#22c55e' }} />
    // Legacy field names (keep compatible during any mixed-traffic period)
    case 'stored':                 return <BookOpen style={{ ...s, color: '#22c55e' }} />
    case 'rated':                  return <Star style={{ ...s, color: '#facc15' }} />
    default:                       return <Activity style={{ ...s, color: '#64748b' }} />
  }
}

// ─── eventLabel ───────────────────────────────────────────────────────────────

export function eventLabel(type: string, meta?: Record<string, string>): string {
  const domain = meta?.domain ? ` · ${meta.domain}` : ''
  switch (type) {
    case 'knowledge_stored':       return `New entry added${domain}`
    case 'knowledge_used':         return `Used knowledge${domain}`
    case 'knowledge_rated':        return `Rated${meta?.rating ? ` ${meta.rating}★` : ''}${domain}`
    case 'enrich_context':         return `Sent a prompt${domain}`
    case 'approved':               return `Entry approved${domain}`
    case 'rejected':               return `Entry rejected${domain}`
    case 'pipeline_complete':      return `Pipeline complete — ${meta?.clusters ?? '?'} clusters`
    case 'improvement_drafted':    return `Improvement drafted${domain}`
    case 'agent_generated':        return `Agent generated${domain}`
    case 'signin':                 return `Signed in`
    case 'todo_created':           return `Created todo${meta?.title ? ` "${meta.title}"` : ''}`
    case 'todo_completed':         return `Completed todo${meta?.title ? ` "${meta.title}"` : ''}`
    // Legacy
    case 'stored':                 return `New entry added${domain}`
    case 'rated':                  return `Entry rated${meta?.rating ? ` ${meta.rating}★` : ''}${domain}`
    default:                       return type
  }
}

// ─── mergeEvents ─────────────────────────────────────────────────────────────
// Merge fresh events at top, keep newest-first, cap at maxLen.

export function mergeEvents(
  prev: ActivityEvent[],
  next: ActivityEvent[],
  maxLen = 100,
): ActivityEvent[] {
  const seenIds = new Set(prev.map(e => e.id))
  const fresh = next.filter(e => !seenIds.has(e.id))
  return [...fresh, ...prev].slice(0, maxLen)
}
