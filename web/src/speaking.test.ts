import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Speaking, rms } from './speaking.ts'

test('opens on loud frames and holds through short gaps', () => {
  const s = new Speaking(0.02, 300)
  assert.equal(s.update(0.001, 0), false)
  assert.equal(s.update(0.1, 100), true)
  assert.equal(s.update(0.001, 350), true) // 250 ms of silence: still speaking
  assert.equal(s.update(0.001, 401), false) // > 300 ms: closed
})

test('rms of a sine is amplitude / sqrt(2)', () => {
  const b = new Float32Array(480).map((_, i) => 0.5 * Math.sin((2 * Math.PI * i) / 48))
  assert.ok(Math.abs(rms(b) - 0.5 / Math.SQRT2) < 1e-3)
})
