// HTTP client for call-signaling behind the same-origin /api proxy.
// The access token lives only in this module's memory (never in storage);
// the refresh token is an httpOnly cookie the browser sends by itself.

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

type Session = { accessToken: string; userId: string }

let session: Session | null = null
let refreshing: Promise<Session | null> | null = null
let onLost: () => void = () => {}

export const currentUser = () => session?.userId ?? null
export const onSessionLost = (fn: () => void) => { onLost = fn }

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

async function post(path: string, body?: unknown): Promise<Response> {
  return fetch('/api' + path, {
    method: 'POST',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

async function fail(r: Response): Promise<never> {
  const j = await r.json().catch(() => ({}))
  throw new ApiError(r.status, j.error ?? r.statusText)
}

async function takeSession(r: Response): Promise<Session> {
  if (!r.ok) await fail(r)
  const j = await r.json()
  session = { accessToken: j.accessToken, userId: j.userId }
  return session
}

// refresh rotates the refresh cookie. Concurrent callers share one request
// (single flight). Another tab may have rotated the cookie a moment ago: the
// server answers 401 inside its grace window, so retry once after 500 ms.
export function refresh(): Promise<Session | null> {
  refreshing ??= (async () => {
    for (let attempt = 0; attempt < 2; attempt++) {
      const r = await post('/auth/refresh')
      if (r.ok) return takeSession(r)
      if (r.status !== 401) await fail(r)
      if (attempt === 0) await sleep(500)
    }
    session = null
    return null
  })().finally(() => { refreshing = null })
  return refreshing
}

export async function login(username: string, password: string) {
  return takeSession(await post('/auth/login', { username, password }))
}

export async function register(username: string, password: string) {
  const r = await post('/auth/register', { username, password })
  if (!r.ok) await fail(r)
}

export async function logout() {
  session = null
  await post('/auth/logout').catch(() => {})
}

// api calls an authenticated endpoint. On 401 it refreshes once and retries;
// if that fails the session is gone and the app goes back to the login screen.
export async function api<T = unknown>(
  path: string,
  init: { method?: string; body?: unknown; keepalive?: boolean } = {},
): Promise<T> {
  const go = () =>
    fetch('/api' + path, {
      method: init.method ?? 'GET',
      keepalive: init.keepalive,
      headers: {
        ...(init.body !== undefined && { 'Content-Type': 'application/json' }),
        ...(session && { Authorization: `Bearer ${session.accessToken}` }),
      },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    })
  let r = await go()
  if (r.status === 401) {
    if (!(await refresh())) {
      onLost()
      throw new ApiError(401, 'unauthorized')
    }
    r = await go()
  }
  if (!r.ok) await fail(r)
  return (r.status === 204 ? undefined : await r.json()) as T
}
