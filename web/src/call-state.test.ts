import { test } from 'node:test'
import assert from 'node:assert/strict'
import { initial, reduce, type Call } from './call-state.ts'

const call: Call = {
  callId: 'c1', status: 'ACTIVE', kind: 'group', callerId: 'ana', participants: ['ana', 'bob'],
  muted: ['bob'], deafened: [], taken: 2, capacity: 10,
}
const ev = (type: string, data: Record<string, unknown>) => ({ t: 'event' as const, type, data, at: 1 })

test('joins, leaves and mute state follow SSE events', () => {
  let s = reduce(initial, { t: 'snapshot', call })
  s = reduce(s, ev('participant_joined', { callId: 'c1', userId: 'carl', participants: ['ana', 'bob', 'carl'] }))
  assert.deepEqual(s.call!.participants, ['ana', 'bob', 'carl'])
  s = reduce(s, ev('participant_state', { callId: 'c1', userId: 'carl', muted: true, deafened: true }))
  assert.deepEqual([s.call!.muted, s.call!.deafened], [['bob', 'carl'], ['carl']])
  s = reduce(s, ev('participant_left', { callId: 'c1', userId: 'carl' }))
  assert.deepEqual([s.call!.participants, s.call!.muted, s.call!.deafened, s.call!.taken], [['ana', 'bob'], ['bob'], [], 2])
})

test('events of other calls are logged but ignored', () => {
  let s = reduce(initial, { t: 'snapshot', call })
  s = reduce(s, ev('call_ended', { callId: 'other' }))
  assert.equal(s.call!.status, 'ACTIVE')
  assert.equal(s.log.length, 1)
})

test('failure closes the call with the backend reason', () => {
  let s = reduce(initial, { t: 'snapshot', call: { ...call, status: 'PENDING' } })
  s = reduce(s, ev('call_failed', { callId: 'c1', reason: 'relay timeout' }))
  assert.deepEqual([s.phase, s.reason, s.call!.status], ['closed', 'relay timeout', 'FAILED'])
})
