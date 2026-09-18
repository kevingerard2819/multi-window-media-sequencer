import test from 'node:test'
import assert from 'node:assert/strict'
import { currentItem, normalElapsed } from '../src/playback.ts'

const cycle = 5 * 60 * 60 * 1000
const items = [
  { id: 'a', durationMs: 10_000 },
  { id: 'b', durationMs: 20_000 },
]

test('short playlists repeat through the five-hour cycle', () => {
  assert.equal(currentItem(items, 0, cycle)?.item.id, 'a')
  assert.equal(currentItem(items, 12_000, cycle)?.item.id, 'b')
  assert.equal(currentItem(items, 31_000, cycle)?.item.id, 'a')
  assert.equal(currentItem(items, cycle - 1_000, cycle)?.item.id, 'b')
  assert.equal(currentItem(items, cycle, cycle)?.item.id, 'a')
})

test('sync freezes normal playback and resumes at the same position', () => {
  const state = { serverTime: 13_000, normalElapsedMs: 12_000, sync: { startsAt: 12_000, endsAt: 17_000 } }
  assert.equal(normalElapsed(state, 16_000), 12_000)
  assert.equal(normalElapsed(state, 17_000), 12_000)
  assert.equal(normalElapsed(state, 19_000), 14_000)
})

test('append phase offset preserves the current item until the cycle resets', () => {
  const before = currentItem(items, 32_000, cycle)
  const expanded = [...items, { id: 'c', durationMs: 15_000 }]
  const phaseOffset = 2_000 - 32_000
  const after = currentItem(expanded, 32_000, cycle, phaseOffset, 0)
  assert.equal(after?.item.id, before?.item.id)
  assert.equal(after?.offsetMs, before?.offsetMs)
  assert.equal(currentItem(expanded, cycle, cycle, phaseOffset, 0)?.item.id, 'a')
})
