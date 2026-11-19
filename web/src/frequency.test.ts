import { test } from 'node:test'
import assert from 'node:assert/strict'
import { frequency } from './frequency.ts'

test('frequency is stable, in band, 3 decimals', () => {
  const id = '6f1c2a3e-0b7d-4e2a-9a51-2f3c4d5e6f70'
  assert.equal(frequency(id), frequency(id))
  assert.notEqual(frequency(id), frequency(id.replace('6f', '7f')))
  for (const f of [frequency(id), frequency('x'), frequency('')]) {
    assert.match(f, /^1\d\d\.\d{3}$/)
  }
})
