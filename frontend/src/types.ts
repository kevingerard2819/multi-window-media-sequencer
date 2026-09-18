export type Media = { id: string; name: string; type: 'image' | 'video' | 'blank'; url: string }
export type Item = { id: string; media: Media; durationMs: number }
export type Window = { id: string; name: string; items: Item[]; phaseOffsetMs: number; phaseOffsetCycle: number }
export type SyncEvent = { id: string; media: Media; startsAt: number; endsAt: number }
export type State = {
  serverTime: number
  cycleMs: number
  normalElapsedMs: number
  windows: Window[]
  sync: SyncEvent | null
}
