// One SSE stream per tab (GET /api/notifications?ticket=). EventSource cannot
// send headers, so it uses a 60 s ticket; its built-in retry would reuse a dead
// ticket, so we reconnect ourselves with a fresh one and exponential backoff.
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { api } from './api.ts'

export const EVENTS = [
  'incoming_call', 'call_active', 'call_failed', 'call_ended', 'participant_joined',
  'participant_left', 'left_call', 'relay_moved', 'participant_state',
] as const

export type LiveEvent = { type: string; data: Record<string, unknown>; at: number }
export type Invite = { callId: string; from: string; at: number }

type Live = {
  connected: boolean
  reconnects: number // bumps after every reconnect: screens resync with GET
  invites: Invite[]
  dismiss: (callId: string) => void
  subscribe: (fn: (e: LiveEvent) => void) => () => void
}

const Ctx = createContext<Live | null>(null)

export function LiveProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false)
  const [reconnects, setReconnects] = useState(0)
  const [invites, setInvites] = useState<Invite[]>([])
  const subs = useRef(new Set<(e: LiveEvent) => void>())

  useEffect(() => {
    let es: EventSource | null = null
    let stopped = false
    let wait = 1000
    let timer: ReturnType<typeof setTimeout>
    let first = true

    const emit = (e: LiveEvent) => {
      if (e.type === 'incoming_call') {
        const inv = { callId: e.data.callId as string, from: e.data.from as string, at: e.at }
        setInvites((xs) => [inv, ...xs.filter((x) => x.callId !== inv.callId)])
      }
      if (e.type === 'call_ended' || e.type === 'call_failed') {
        setInvites((xs) => xs.filter((x) => x.callId !== e.data.callId))
      }
      subs.current.forEach((fn) => fn(e))
    }

    async function open() {
      try {
        const { ticket } = await api<{ ticket: string }>('/sse-ticket', { method: 'POST' })
        if (stopped) return
        es = new EventSource('/api/notifications?ticket=' + encodeURIComponent(ticket))
        es.onopen = () => {
          wait = 1000
          setConnected(true)
          if (!first) setReconnects((n) => n + 1)
          first = false
        }
        for (const type of EVENTS) {
          es.addEventListener(type, (m) => emit({ type, data: JSON.parse((m as MessageEvent).data), at: Date.now() }))
        }
        es.onerror = () => retry()
      } catch {
        retry()
      }
    }
    function retry() {
      es?.close()
      es = null
      setConnected(false)
      if (stopped) return
      clearTimeout(timer)
      timer = setTimeout(open, wait)
      wait = Math.min(wait * 2, 10_000)
    }

    open()
    return () => {
      stopped = true
      clearTimeout(timer)
      es?.close()
    }
  }, [])

  const live: Live = {
    connected,
    reconnects,
    invites,
    dismiss: (id) => setInvites((xs) => xs.filter((x) => x.callId !== id)),
    subscribe: (fn) => {
      subs.current.add(fn)
      return () => subs.current.delete(fn)
    },
  }
  return <Ctx.Provider value={live}>{children}</Ctx.Provider>
}

export function useLive(): Live {
  const l = useContext(Ctx)
  if (!l) throw new Error('useLive outside LiveProvider')
  return l
}
