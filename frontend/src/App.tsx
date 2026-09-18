import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { currentItem, formatTime, normalElapsed, serverNow } from './playback'
import type { Item, Media, State, Window } from './types'

type Snapshot = { state: State; receivedAt: number }

async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...options?.headers } })
  const body = await response.json()
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`)
  return body as T
}

function mediaLabel(media: Media) { return media.type === 'blank' ? 'Blank' : media.type === 'video' ? 'Video' : 'Image' }

function MediaView({ media, offsetMs, playing }: { media: Media; offsetMs: number; playing: boolean }) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [media.id, media.url])
  useEffect(() => {
    const video = videoRef.current
    if (!video || !playing) return
    const align = () => {
      if (!Number.isFinite(video.duration) || video.duration <= 0) return
      const target = (offsetMs / 1000) % video.duration
      if (Math.abs(video.currentTime - target) > 0.65) video.currentTime = target
      video.play().catch(() => {})
    }
    align()
    video.addEventListener('loadedmetadata', align)
    return () => video.removeEventListener('loadedmetadata', align)
  }, [media.id, media.url, offsetMs, playing])

  if (media.type === 'blank') return <div className="blank-state"><div className="blank-ring" /><span>QUIET INTERVAL</span></div>
  if (failed) return <div className="media-error"><span>Media unavailable</span><small>{media.name}</small></div>
  if (media.type === 'image') return <img className="media-image" src={media.url} alt={media.name} onError={() => setFailed(true)} />
  return <video ref={videoRef} className="media-video" src={media.url} muted playsInline autoPlay loop onError={() => setFailed(true)} />
}

function Panel({ window, state, now, onAdd }: { window: Window; state: State; now: number; onAdd: (id: string) => void }) {
  const syncActive = !!state.sync && now >= state.sync.startsAt && now < state.sync.endsAt
  const syncPending = !!state.sync && now < state.sync.startsAt
  const normal = currentItem(window.items, normalElapsed(state, now), state.cycleMs, window.phaseOffsetMs, window.phaseOffsetCycle)
  const media = syncActive ? state.sync!.media : normal?.item.media
  const offset = syncActive ? now - state.sync!.startsAt : normal?.offsetMs ?? 0
  const index = normal?.index ?? -1
  const duration = syncActive ? state.sync!.endsAt - state.sync!.startsAt : normal?.item.durationMs ?? 1
  const progress = Math.min(100, Math.max(0, (offset / duration) * 100))

  return <article className={`panel ${syncActive ? 'panel-sync' : ''}`}>
    <div className="panel-head"><div><span className="panel-dot" /><span className="panel-name">{window.name}</span></div><span className="panel-number">{window.id.replace('window-', 'DISPLAY ')}</span></div>
    <div className="screen">
      {media ? <MediaView key={syncActive ? `sync-${state.sync!.id}` : normal!.item.id} media={media} offsetMs={offset} playing={true} /> : <div className="blank-state"><div className="blank-ring" /><span>NO MEDIA CONFIGURED</span></div>}
      <div className="screen-shade" />
      <span className={`screen-badge ${syncActive ? 'badge-sync' : ''}`}>{syncActive ? '● SYNC PLAYBACK' : syncPending ? '● SYNC QUEUED' : '● LIVE SEQUENCE'}</span>
      <div className="screen-caption"><div><small>NOW PLAYING</small><strong>{media?.name ?? 'Empty playlist'}</strong></div><span>{media ? mediaLabel(media) : '—'}</span></div>
    </div>
    <div className="progress-track"><div style={{ width: `${progress}%` }} /></div>
    <div className="panel-footer"><span>{window.items.length} ITEMS IN SEQUENCE</span><span>{formatTime(offset)} / {formatTime(duration)}</span></div>
    <div className="playlist"><div className="playlist-heading"><span>PLAYLIST</span><button type="button" onClick={() => onAdd(window.id)}>+ Add media</button></div>
      {window.items.length === 0 && <div className="empty-list">Add an item to start playback.</div>}
      {window.items.map((item: Item, itemIndex: number) => <div className={`playlist-row ${!syncActive && itemIndex === index ? 'row-active' : ''}`} key={item.id}><span className="row-index">{String(itemIndex + 1).padStart(2, '0')}</span><span className="row-name">{item.media.name}</span><span className="row-kind">{mediaLabel(item.media)}</span><span className="row-duration">{formatTime(item.durationMs)}</span></div>)}
    </div>
  </article>
}

function AddModal({ window, close, done }: { window: Window; close: () => void; done: () => void }) {
  const [name, setName] = useState('')
  const [type, setType] = useState<Media['type']>('image')
  const [url, setUrl] = useState('')
  const [seconds, setSeconds] = useState(10)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  async function submit(event: FormEvent) {
    event.preventDefault(); setError(''); setSaving(true)
    try { await api(`/api/windows/${window.id}/items`, { method: 'POST', body: JSON.stringify({ name, type, url: type === 'blank' ? '' : url, durationMs: seconds * 1000 }) }); done() }
    catch (err) { setError((err as Error).message) }
    finally { setSaving(false) }
  }
  return <div className="modal-backdrop" onMouseDown={close}><div className="modal" onMouseDown={event => event.stopPropagation()} role="dialog" aria-modal="true" aria-label="Add media"><div className="modal-top"><div><small>EDIT PLAYLIST</small><h2>Add to {window.name}</h2></div><button className="icon-button" onClick={close} aria-label="Close">×</button></div><form onSubmit={submit}>
    <label>Media name<input required maxLength={120} value={name} onChange={event => setName(event.target.value)} placeholder="e.g. Morning highlights" /></label>
    <div className="form-grid"><label>Type<select value={type} onChange={event => setType(event.target.value as Media['type'])}><option value="image">Image</option><option value="video">Video</option><option value="blank">Blank interval</option></select></label><label>Duration (seconds)<input type="number" min="1" max="3600" required value={seconds} onChange={event => setSeconds(Number(event.target.value))} /></label></div>
    {type !== 'blank' && <label>Media URL<input required type="url" value={url} onChange={event => setUrl(event.target.value)} placeholder="https://example.com/media.jpg" /></label>}
    <p className="form-hint">New media is appended to this display's playlist. Publicly accessible URLs work best.</p>
    {error && <p className="form-error">{error}</p>}
    <div className="modal-actions"><button className="button-muted" type="button" onClick={close}>Cancel</button><button className="button-primary" disabled={saving} type="submit">{saving ? 'Adding…' : 'Add media ↗'}</button></div>
  </form></div></div>
}

function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [tick, setTick] = useState(Date.now())
  const [error, setError] = useState('')
  const [addWindowId, setAddWindowId] = useState<string | null>(null)
  const [syncMediaId, setSyncMediaId] = useState('')
  const [syncSeconds, setSyncSeconds] = useState(10)
  const [syncing, setSyncing] = useState(false)
  const refresh = async () => { try { const state = await api<State>('/api/state'); setSnapshot({ state, receivedAt: Date.now() }); setError('') } catch (err) { setError((err as Error).message) } }
  useEffect(() => {
    void refresh()
    const events = new EventSource('/api/events')
    events.addEventListener('change', () => { void refresh() })
    const clock = window.setInterval(() => setTick(Date.now()), 250)
    const fallback = window.setInterval(() => { void refresh() }, 30000)
    return () => { events.close(); window.clearInterval(clock); window.clearInterval(fallback) }
  }, [])
  const state = snapshot?.state
  const now = state && snapshot ? serverNow(state, tick, snapshot.receivedAt) : tick
  const mediaOptions = useMemo(() => { const found = new Map<string, Media>(); state?.windows.forEach(window => window.items.forEach(item => found.set(item.media.id, item.media))); return [...found.values()] }, [state])
  useEffect(() => { if (!mediaOptions.some(media => media.id === syncMediaId)) setSyncMediaId(mediaOptions[0]?.id ?? '') }, [mediaOptions, syncMediaId])
  const activeSync = state?.sync && now < state.sync.endsAt
  const currentCycle = state ? normalElapsed(state, now) % state.cycleMs : 0
  const modalWindow = state?.windows.find(window => window.id === addWindowId)
  async function startSync() {
    if (!syncMediaId) return
    setSyncing(true); setError('')
    try { await api('/api/sync', { method: 'POST', body: JSON.stringify({ mediaId: syncMediaId, durationMs: syncSeconds * 1000 }) }); await refresh() }
    catch (err) { setError((err as Error).message) }
    finally { setSyncing(false) }
  }

  return <main className="app-shell">
    <header className="topbar"><div className="brand"><div className="brand-mark"><span /><span /><span /></div><strong>SEQUENCE<span>STUDIO</span></strong></div><div className="topbar-right"><span className="connection"><i /> {error ? 'CONNECTION ISSUE' : 'SYSTEM ONLINE'}</span><span className="topbar-time">{new Date(tick).toLocaleTimeString()}</span></div></header>
    <section className="cycle-strip" aria-label="Playback cycle"><div className="cycle-card"><small>CURRENT CYCLE</small><strong>{formatTime(currentCycle)}</strong><div className="cycle-track"><div style={{ width: `${state ? (currentCycle / state.cycleMs) * 100 : 0}%` }} /></div><span>OF 05:00:00 CYCLE</span></div></section>
    <section className="control-card"><div className="control-title"><div className="control-icon">↗</div><div><small>GLOBAL OVERRIDE</small><h2>Sync playback</h2><p>Show the same media across all displays for a set duration.</p></div></div><div className="control-fields"><label>MEDIA ITEM<select value={syncMediaId} onChange={event => setSyncMediaId(event.target.value)} disabled={!state || !!activeSync}>{mediaOptions.map(media => <option key={media.id} value={media.id}>{media.name} · {mediaLabel(media)}</option>)}</select></label><label>DURATION<select value={syncSeconds} onChange={event => setSyncSeconds(Number(event.target.value))} disabled={!!activeSync}><option value={5}>5 seconds</option><option value={10}>10 seconds</option><option value={15}>15 seconds</option><option value={30}>30 seconds</option><option value={60}>1 minute</option></select></label><button className="sync-button" onClick={startSync} disabled={!syncMediaId || !!activeSync || syncing}>{activeSync ? 'Sync in progress' : syncing ? 'Starting…' : 'Sync all screens ↗'}</button></div></section>
    {error && <div className="error-banner">{error} <button onClick={() => void refresh()}>Retry</button></div>}
    <div className="section-heading"><div><span className="section-kicker">DISPLAY NETWORK</span><h2>Active windows <span>({state?.windows.length ?? 0})</span></h2></div><span className="section-meta">AUTO-REFRESH ENABLED <i /></span></div>
    {state ? <section className="panel-grid">{state.windows.map(window => <Panel key={window.id} window={window} state={state} now={now} onAdd={setAddWindowId} />)}</section> : <div className="loading">Connecting to display network…</div>}
    <footer><span>SEQUENCE STUDIO © 2026</span><span>CONTINUOUS PLAYBACK · 5 HOUR CYCLE</span></footer>
    {modalWindow && <AddModal window={modalWindow} close={() => setAddWindowId(null)} done={() => { setAddWindowId(null); void refresh() }} />}
  </main>
}

export default App
