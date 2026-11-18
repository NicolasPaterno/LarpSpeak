// Run: npm test (node --test, Node 24 strips the types).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { api, currentUser, refresh, onSessionLost } from './api.ts'

type Call = { url: string; auth?: string }

function fakeFetch(handler: (url: string, auth?: string) => Response) {
  const calls: Call[] = []
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    const auth = (init?.headers as Record<string, string> | undefined)?.Authorization
    calls.push({ url, auth })
    return handler(url, auth)
  }) as typeof fetch
  return calls
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

test('concurrent 401s trigger exactly one refresh, then retry with the new token', async () => {
  const calls = fakeFetch((url, auth) => {
    if (url === '/api/auth/refresh') return json(200, { accessToken: 'new', userId: 'ana' })
    return auth === 'Bearer new' ? json(200, { ok: true }) : json(401, { error: 'unauthorized' })
  })
  const results = await Promise.all([api('/me'), api('/me'), api('/me')])
  assert.equal(results.length, 3)
  assert.equal(calls.filter((c) => c.url === '/api/auth/refresh').length, 1)
  assert.equal(currentUser(), 'ana')
})

test('refresh retries once on 401 (other tab rotated), then gives up', async () => {
  const calls = fakeFetch(() => json(401, { error: 'invalid credentials' }))
  assert.equal(await refresh(), null)
  assert.equal(calls.length, 2)
  assert.equal(currentUser(), null)
})

test('lost session calls the logout hook', async () => {
  fakeFetch(() => json(401, { error: 'unauthorized' }))
  let lost = false
  onSessionLost(() => { lost = true })
  await assert.rejects(api('/me'), { status: 401 })
  assert.ok(lost)
})
