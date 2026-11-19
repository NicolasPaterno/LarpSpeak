// A call is shown as a radio frequency: the same callId always tunes to the
// same channel, e.g. "147.320". Purely cosmetic; the callId stays the real key.
export function frequency(callId: string): string {
  let h = 2166136261 // FNV-1a
  for (const ch of callId) h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0
  const khz = 100_000 + (h % 100_000) // 100.000 .. 199.999 MHz
  return (khz / 1000).toFixed(3)
}
