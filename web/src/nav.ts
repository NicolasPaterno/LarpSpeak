// Three routes (/, /c/<callId>, anything else -> /): a router library is not worth it.
export function navigate(to: string) {
  if (location.pathname === to) return
  history.pushState(null, '', to)
  dispatchEvent(new PopStateEvent('popstate'))
}

const CALL_ID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/

/** Accepts a bare callId or any link that contains one. */
export function parseCallId(input: string): string | null {
  return input.trim().toLowerCase().match(CALL_ID)?.[0] ?? null
}

export const callLink = (callId: string) => `${location.origin}/c/${callId}`
