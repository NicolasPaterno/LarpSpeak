// Call screen state, fed by HTTP snapshots, SSE events and the voice engine.
// Pure: tested with node --test.

export type CallStatus = 'PENDING' | 'ACTIVE' | 'FAILED' | 'ENDED'

export type Call = {
  callId: string
  status: CallStatus
  kind: 'direct' | 'group'
  callerId: string
  participants: string[]
  invited?: string[]
  muted: string[]
  deafened: string[]
  taken: number
  capacity: number
  reason?: string
  relayHost?: string
  relayPort?: number
}

export type Phase =
  | 'loading' // first GET
  | 'tuning' // PENDING: relay being allocated
  | 'connecting' // WebRTC offer/ICE
  | 'live'
  | 'rerouting' // relay failover / reconnect
  | 'closed' // FAILED, ENDED, or I was removed
  | 'error'

export type LogEntry = { at: number; source: 'sse' | 'http' | 'rtc'; type: string; data?: unknown }

export type State = { call: Call | null; phase: Phase; reason?: string; log: LogEntry[] }

export type Action =
  | { t: 'snapshot'; call: Call }
  | { t: 'phase'; phase: Phase; reason?: string }
  | { t: 'event'; type: string; data: Record<string, unknown>; at: number }
  | { t: 'log'; entry: LogEntry }

export const initial: State = { call: null, phase: 'loading', log: [] }

const LOG_MAX = 200
const without = (xs: string[], x: string) => xs.filter((y) => y !== x)
const toggle = (xs: string[], x: string, on: boolean) => (on ? [...without(xs, x), x] : without(xs, x))

function logged(s: State, e: LogEntry): State {
  return { ...s, log: [e, ...s.log].slice(0, LOG_MAX) }
}

export function reduce(s: State, a: Action): State {
  switch (a.t) {
    case 'snapshot':
      return { ...s, call: a.call }
    case 'phase':
      return { ...s, phase: a.phase, reason: a.reason }
    case 'log':
      return logged(s, a.entry)
    case 'event': {
      const next = logged(s, { at: a.at, source: 'sse', type: a.type, data: a.data })
      const c = next.call
      if (!c || a.data.callId !== c.callId) return next
      const d = a.data
      switch (a.type) {
        case 'participant_joined':
        case 'participant_left': {
          const participants = (d.participants as string[] | undefined) ??
            (a.type === 'participant_joined' ? [...without(c.participants, d.userId as string), d.userId as string] : without(c.participants, d.userId as string))
          const gone = a.type === 'participant_left' ? (d.userId as string) : ''
          return {
            ...next,
            call: { ...c, participants, taken: participants.length, muted: without(c.muted, gone), deafened: without(c.deafened, gone) },
          }
        }
        case 'participant_state':
          return {
            ...next,
            call: {
              ...c,
              muted: toggle(c.muted, d.userId as string, !!d.muted),
              deafened: toggle(c.deafened, d.userId as string, !!d.deafened),
            },
          }
        case 'call_active':
          return { ...next, call: { ...c, status: 'ACTIVE' } }
        case 'call_failed':
          return { ...next, call: { ...c, status: 'FAILED', reason: d.reason as string }, phase: 'closed', reason: d.reason as string }
        case 'call_ended':
          return { ...next, call: { ...c, status: 'ENDED', reason: d.reason as string }, phase: 'closed', reason: d.reason as string }
        case 'left_call':
          return { ...next, phase: 'closed', reason: d.reason as string }
        case 'relay_moved':
          return { ...next, call: { ...c, relayHost: d.relayHost as string, relayPort: d.relayPort as number } }
      }
      return next
    }
  }
}
