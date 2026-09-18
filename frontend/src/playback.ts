import type { Item, State } from './types'

export function serverNow(state: State, localNow: number, receivedAt: number): number {
  return state.serverTime + (localNow - receivedAt)
}

function pausedUntil(state: State, at: number): number {
  if (!state.sync) return 0
  return Math.max(0, Math.min(at, state.sync.endsAt) - state.sync.startsAt)
}

export function normalElapsed(state: State, at: number): number {
  return Math.max(0, state.normalElapsedMs + at - state.serverTime - (pausedUntil(state, at) - pausedUntil(state, state.serverTime)))
}

export function currentItem(items: Item[], elapsed: number, cycleMs: number, phaseOffsetMs = 0, phaseOffsetCycle = -1): { item: Item; offsetMs: number; index: number } | null {
  const valid = items.filter(item => item.durationMs > 0)
  if (!valid.length) return null
  const cyclePosition = elapsed % cycleMs
  const total = valid.reduce((sum, item) => sum + item.durationMs, 0)
  const offset = Math.floor(elapsed / cycleMs) === phaseOffsetCycle ? phaseOffsetMs : 0
  let position = ((cyclePosition + offset) % total + total) % total
  for (let index = 0; index < valid.length; index++) {
    const item = valid[index]
    if (position < item.durationMs) return { item, offsetMs: position, index }
    position -= item.durationMs
  }
  return { item: valid[0], offsetMs: 0, index: 0 }
}

export function formatTime(milliseconds: number): string {
  const seconds = Math.floor(milliseconds / 1000)
  return `${String(Math.floor(seconds / 3600)).padStart(2, '0')}:${String(Math.floor(seconds / 60) % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}
