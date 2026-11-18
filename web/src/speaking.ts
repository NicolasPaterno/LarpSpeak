// Speaking detection from an audio level (RMS 0..1). Opens fast above the
// threshold and closes only after `hangMs` of silence, so the ring does not
// flicker between words.
export class Speaking {
  private until = 0
  private threshold: number
  private hangMs: number
  constructor(threshold = 0.02, hangMs = 300) {
    this.threshold = threshold
    this.hangMs = hangMs
  }

  update(rms: number, now: number): boolean {
    if (rms >= this.threshold) this.until = now + this.hangMs
    return now < this.until
  }
}

/** RMS of a time-domain buffer from AnalyserNode.getFloatTimeDomainData. */
export function rms(buf: Float32Array): number {
  let sum = 0
  for (const v of buf) sum += v * v
  return Math.sqrt(sum / buf.length)
}
