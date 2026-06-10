import { useEffect, useRef, useState, useCallback } from 'react'
import type { ActivityEvent, ActorRef } from '@/lib/api'
import { mergeEvents } from '@/lib/activity'

// ─── Types ────────────────────────────────────────────────────────────────────

interface ActorEntry {
  actor: ActorRef
  lastSeen: number
}

export interface ActivityStreamState {
  events: ActivityEvent[]
  online: ActorRef[]
  onlineCount: number
  connected: boolean
}

// ─── SSE frame parser ─────────────────────────────────────────────────────────

interface SSEFrame {
  event: string
  data: string
}

function parseFrames(raw: string): SSEFrame[] {
  // Normalize CRLF and bare CR to LF so proxy/CDN line-endings don't
  // prevent frame splitting or line parsing.
  raw = raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n')

  // Each SSE message is separated by a blank line (\n\n).
  const frames: SSEFrame[] = []
  const blocks = raw.split(/\n\n+/)
  for (const block of blocks) {
    if (!block.trim()) continue
    let eventName = 'message'
    const dataLines: string[] = []
    for (const line of block.split('\n')) {
      if (line.startsWith(':')) continue // keepalive comment
      if (line.startsWith('event:')) {
        eventName = line.slice(6).trim()
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trim())
      }
    }
    if (dataLines.length > 0) {
      frames.push({ event: eventName, data: dataLines.join('\n') })
    }
  }
  return frames
}

// ─── Hook ─────────────────────────────────────────────────────────────────────

const ONLINE_EXPIRY_MS = 60_000   // 60 s — actor considered offline
const PRUNE_INTERVAL_MS = 5_000   // check every 5 s
const MAX_EVENTS = 100
const MAX_BACKOFF_MS = 15_000
const BATCH_FLUSH_DELAY_MS = 80   // coalesce rapid events

export function useActivityStream(): ActivityStreamState {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [online, setOnline] = useState<ActorRef[]>([])
  const [onlineCount, setOnlineCount] = useState(0)
  const [connected, setConnected] = useState(false)

  // Mutable refs that don't trigger re-renders
  const actorMapRef = useRef<Map<string, ActorEntry>>(new Map())
  const abortRef    = useRef<AbortController | null>(null)
  const retryRef    = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pruneRef    = useRef<ReturnType<typeof setInterval> | null>(null)
  const batchRef    = useRef<ActivityEvent[]>([])
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef  = useRef(true)
  const backoffRef  = useRef(1000)

  // ── Helpers ──

  const upsertActor = useCallback((actor: ActorRef, lastSeen: number) => {
    actorMapRef.current.set(actor.id, { actor, lastSeen })
  }, [])

  const publishOnline = useCallback(() => {
    const list = Array.from(actorMapRef.current.values()).map(e => e.actor)
    setOnline(list)
  }, [])

  const pruneActors = useCallback(() => {
    const cutoff = Date.now() - ONLINE_EXPIRY_MS
    let changed = false
    actorMapRef.current.forEach((entry, id) => {
      if (entry.lastSeen < cutoff) {
        actorMapRef.current.delete(id)
        changed = true
      }
    })
    if (changed) publishOnline()
  }, [publishOnline])

  // Flush buffered events into state (called via rAF/setTimeout)
  const flushBatch = useCallback(() => {
    flushTimerRef.current = null
    const batch = batchRef.current
    if (batch.length === 0) return
    batchRef.current = []
    setEvents(prev => mergeEvents(prev, batch, MAX_EVENTS))
  }, [])

  const scheduleFlush = useCallback(() => {
    if (flushTimerRef.current !== null) return
    flushTimerRef.current = setTimeout(flushBatch, BATCH_FLUSH_DELAY_MS)
  }, [flushBatch])

  // ── Stream connector ──

  const connect = useCallback(async () => {
    if (!mountedRef.current) return

    const ctrl = new AbortController()
    abortRef.current = ctrl

    try {
      const key = localStorage.getItem('tkm_api_key')
      const headers: Record<string, string> = {}
      if (key) headers['Authorization'] = `Bearer ${key}`

      const resp = await fetch('/api/activity/stream', {
        headers,
        signal: ctrl.signal,
      })

      // Auth failures must not trigger a reconnect loop.
      if (resp.status === 401) {
        // Key is invalid / expired — clear it and redirect to login.
        localStorage.removeItem('tkm_api_key')
        setConnected(false)
        window.location.replace('/login')
        return // deliberately stop; no scheduleReconnect
      }
      if (resp.status === 403) {
        // Forbidden — stop silently; do not redirect.
        setConnected(false)
        return // deliberately stop; no scheduleReconnect
      }

      if (!resp.ok || !resp.body) {
        throw new Error(`SSE ${resp.status}`)
      }

      if (!mountedRef.current) return
      setConnected(true)
      backoffRef.current = 1000

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''

      // eslint-disable-next-line no-constant-condition
      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })

        // Process complete frames (separated by \n\n)
        const splitAt = buf.lastIndexOf('\n\n')
        if (splitAt === -1) continue

        const chunk = buf.slice(0, splitAt + 2)
        buf = buf.slice(splitAt + 2)

        const frames = parseFrames(chunk)
        for (const frame of frames) {
          if (!mountedRef.current) break
          handleFrame(frame)
        }
      }

      // Stream ended cleanly — reconnect
      if (mountedRef.current) {
        setConnected(false)
        scheduleReconnect()
      }
    } catch (err) {
      if (!mountedRef.current) return
      const isAbort = err instanceof DOMException && err.name === 'AbortError'
      if (isAbort) return // intentional unmount, don't reconnect
      setConnected(false)
      scheduleReconnect()
    }
  // handleFrame is defined below and referenced here; it is stable across renders
  // because it only uses refs (not closures over state).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ── Frame handler (uses only refs, never stale) ──
  // Defined as a plain function inside the hook — refs ensure no stale-closure issues.

  function handleFrame(frame: SSEFrame) {
    try {
      const payload = JSON.parse(frame.data)

      if (frame.event === 'snapshot') {
        // Seed online roster
        const onlineActors: ActorRef[] = payload.online ?? []
        const now = Date.now()
        actorMapRef.current.clear()
        for (const a of onlineActors) {
          upsertActor(a, now)
        }
        publishOnline()
        setOnlineCount(onlineActors.length)

        // Seed events feed (recent is oldest-first from server)
        const recent: ActivityEvent[] = (payload.recent ?? []).filter(
          (e: ActivityEvent) => e.type !== 'presence'
        )
        // Reverse to newest-first then merge
        const newestFirst = [...recent].reverse()
        setEvents(prev => mergeEvents(prev, newestFirst, MAX_EVENTS))

      } else if (frame.event === 'activity') {
        const ev: ActivityEvent = payload
        if (ev.type === 'presence') return

        // Buffer event for batched flush
        batchRef.current.push(ev)
        scheduleFlush()

        // Upsert actor into online roster
        if (ev.actor?.id) {
          upsertActor(ev.actor, Date.now())
          publishOnline()
        }

      } else if (frame.event === 'presence') {
        const ev: ActivityEvent = payload
        const serverCount = ev.meta?.online_count
        if (serverCount !== undefined) {
          setOnlineCount(parseInt(serverCount, 10) || 0)
        }
      }
    } catch {
      // malformed JSON — ignore
    }
  }

  function scheduleReconnect() {
    const delay = Math.min(backoffRef.current, MAX_BACKOFF_MS)
    backoffRef.current = Math.min(backoffRef.current * 2, MAX_BACKOFF_MS)
    retryRef.current = setTimeout(() => {
      if (mountedRef.current) connect()
    }, delay)
  }

  // ── Effects ──

  useEffect(() => {
    mountedRef.current = true

    // Start SSE connection
    connect()

    // Start prune interval
    pruneRef.current = setInterval(pruneActors, PRUNE_INTERVAL_MS)

    return () => {
      mountedRef.current = false
      abortRef.current?.abort()
      if (retryRef.current !== null) clearTimeout(retryRef.current)
      if (pruneRef.current !== null) clearInterval(pruneRef.current)
      if (flushTimerRef.current !== null) clearTimeout(flushTimerRef.current)
    }
  // connect and pruneActors are stable (useCallback with empty deps or ref-only deps)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return { events, online, onlineCount, connected }
}
