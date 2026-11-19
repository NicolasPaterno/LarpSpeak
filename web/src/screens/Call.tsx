import { useCallback, useEffect, useReducer, useRef, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { api, ApiError } from '../api.ts'
import { useAuth } from '../auth.tsx'
import { useLive } from '../live.tsx'
import { callLink, navigate } from '../nav.ts'
import { frequency } from '../frequency.ts'
import { initial, reduce, type Call as CallT, type Phase } from '../call-state.ts'
import { Voice, type AudioPrefs, type Stats } from '../voice.ts'
import { Band, Readout } from '../components/Radio.tsx'
import { TopBar } from '../components/TopBar.tsx'
import { Control, Drawer, OpsPanel, ParticipantTile, SettingsPanel } from '../components/CallParts.tsx'
import { InviteInput } from '../components/InviteInput.tsx'
import { Activity, Copy, Headphones, HeadphonesOff, Mic, MicOff, PhoneOff, Radio, Sliders, UserPlus } from '../components/Icons.tsx'
import { t } from '../strings.ts'

const PREFS_KEY = 'larpspeak.audio'
const PTT_KEY = 'Space'
const defaultPrefs: AudioPrefs = { echoCancel: true, noiseSuppress: true }

function loadPrefs(): AudioPrefs {
  try { return { ...defaultPrefs, ...JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') } } catch { return defaultPrefs }
}
function savePrefs(p: AudioPrefs) {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(p)) } catch { /* private mode: keep in memory */ }
}
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const typing = (e: KeyboardEvent) => e.target instanceof HTMLElement && /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)
const why = (r?: string) => (r && (t.call.reasons[r] ?? r)) || ''

export function Call({ callId }: { callId: string }) {
  const me = useAuth().userId!
  const live = useLive()
  const [s, dispatch] = useReducer(reduce, initial)
  const voice = useRef<Voice | null>(null)
  const phaseRef = useRef<Phase>('loading')
  phaseRef.current = s.phase

  const [speaking, setSpeaking] = useState<Set<string>>(new Set())
  const [level, setLevel] = useState(0)
  const [stats, setStats] = useState<Stats>({})
  const [conn, setConn] = useState('new')
  const [muted, setMuted] = useState(false)
  const [deafened, setDeafened] = useState(false)
  const [ptt, setPtt] = useState(false)
  const [talking, setTalking] = useState(false)
  const [prefs, setPrefs] = useState(loadPrefs)
  const [panel, setPanel] = useState<'settings' | 'ops' | 'invite' | null>(null)
  const [needsGesture, setNeedsGesture] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)

  const log = useCallback((source: 'http' | 'rtc', type: string, data?: unknown) =>
    dispatch({ t: 'log', entry: { at: Date.now(), source, type, data } }), [])
  const phase = (p: Phase, reason?: string) => dispatch({ t: 'phase', phase: p, reason })
  const flash = (m: string) => { setNotice(m); setTimeout(() => setNotice(null), 2500) }

  // Enter the call: read it, wait for the relay, take a seat, start voice.
  useEffect(() => {
    let cancelled = false
    const get = async () => {
      const c = await api<CallT>(`/calls/${callId}`)
      if (!cancelled) dispatch({ t: 'snapshot', call: c })
      return c
    }
    async function enter() {
      try {
        let c = await get()
        log('http', 'GET call', { status: c.status, participants: c.participants })
        while (c.status === 'PENDING' && !cancelled) { // R3: 202 + polling
          phase('tuning')
          await sleep(400)
          c = await get()
        }
        if (cancelled) return
        if (c.status !== 'ACTIVE') return phase('closed', c.reason)
        if (!c.participants.includes(me)) {
          try {
            await api(`/calls/${callId}/join`, { method: 'POST' })
            log('http', 'POST join')
            c = await get()
          } catch (err) {
            if (!(err instanceof ApiError && /already in/.test(err.message))) throw err
          }
        }
        if (cancelled) return
        setMuted(c.muted.includes(me))
        setDeafened(c.deafened.includes(me))
        phase('connecting')
        const v = new Voice(callId, me, c.capacity, prefs, {
          onConnection: (st) => {
            setConn(st)
            if (st === 'connected') phase('live')
            else if (st === 'offering' || st === 'failed' || st === 'disconnected') {
              if (phaseRef.current === 'live' || phaseRef.current === 'rerouting') phase('rerouting')
            }
          },
          onSpeaking: setSpeaking,
          onLevel: setLevel,
          onStats: setStats,
          onLog: (type, data) => log('rtc', type, data),
          onFatal: (reason) => phase('closed', reason),
          onBlockedAutoplay: () => setNeedsGesture(true),
        })
        v.setMuted(c.muted.includes(me))
        v.setDeafened(c.deafened.includes(me))
        voice.current = v
        await v.start()
      } catch (err) {
        if (cancelled) return
        if (err instanceof DOMException && err.name === 'NotAllowedError') return phase('error', t.call.micDenied)
        if (err instanceof ApiError) {
          if (err.status === 404) return phase('error', t.call.notFound)
          if (/full/.test(err.message)) return phase('error', t.call.full)
          if (/not active/.test(err.message)) return phase('closed', t.call.notActive)
        }
        phase('error', (err as Error).message)
      }
    }
    enter()
    return () => {
      cancelled = true
      voice.current?.stop()
      voice.current = null
    }
    // prefs are read once at start; later changes go through setPrefs below
  }, [callId, me, log])

  // SSE: membership, mute states, end of call, relay failover.
  useEffect(() => live.subscribe((e) => {
    dispatch({ t: 'event', type: e.type, data: e.data, at: e.at })
    if (e.data.callId !== callId) return
    if (e.type === 'relay_moved') voice.current?.connect('relay moved') // failover: re-offer to the new pod
    if (['call_ended', 'call_failed', 'left_call'].includes(e.type)) voice.current?.stop()
  }), [live, callId])

  // After an SSE reconnect we may have missed events: resync from the server.
  useEffect(() => {
    if (live.reconnects) api<CallT>(`/calls/${callId}`).then((c) => dispatch({ t: 'snapshot', call: c }), () => {})
  }, [live.reconnects, callId])

  const sendState = (m: boolean, d: boolean) =>
    api(`/calls/${callId}/state`, { method: 'POST', body: { muted: m, deafened: d } }).catch(() => {})
  const toggleMute = () => {
    const m = !muted
    setMuted(m)
    voice.current?.setMuted(m)
    sendState(m, deafened)
  }
  const toggleDeafen = () => {
    const d = !deafened
    setDeafened(d)
    voice.current?.setDeafened(d)
    sendState(muted, d)
  }
  const togglePtt = () => { setPtt(!ptt); voice.current?.setPtt(!ptt) }
  const leave = async () => {
    voice.current?.stop()
    await api(`/calls/${callId}/leave`, { method: 'POST' }).catch(() => {})
    navigate('/')
  }
  const changePrefs = (p: AudioPrefs) => { setPrefs(p); savePrefs(p); voice.current?.setPrefs(p).catch((e) => log('rtc', 'device_error', String(e))) }
  const invite = (u: string) => {
    if (s.call?.participants.includes(u)) return flash(`${u} ${t.call.alreadyHere}`)
    api(`/calls/${callId}/invite`, { method: 'POST', body: { inviteeId: u } })
      .then(() => flash(`${t.call.invited} ${u}`), (e) => flash((e as Error).message))
  }

  // Shortcuts (ignored while typing): M mute, D deafen, hold Space = PTT, I invite, O ops, Esc close.
  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (typing(e) || e.repeat) return
      if (e.code === PTT_KEY && ptt) { e.preventDefault(); setTalking(true); voice.current?.setTalking(true); return }
      const k = e.key.toLowerCase()
      if (k === 'm') toggleMute()
      else if (k === 'd') toggleDeafen()
      else if (k === 'i') setPanel((p) => (p === 'invite' ? null : 'invite'))
      else if (k === 'o') setPanel((p) => (p === 'ops' ? null : 'ops'))
      else if (k === 'escape') setPanel(null)
    }
    const up = (e: KeyboardEvent) => {
      if (e.code === PTT_KEY && ptt) { setTalking(false); voice.current?.setTalking(false) }
    }
    addEventListener('keydown', down)
    addEventListener('keyup', up)
    return () => { removeEventListener('keydown', down); removeEventListener('keyup', up) }
  })

  const c = s.call
  const freq = frequency(callId)
  const closed = s.phase === 'closed' || s.phase === 'error'
  const tone = closed ? 'red' : s.phase === 'live' ? 'signal' : 'amber'
  const others = (c?.participants ?? []).filter((u) => u !== me)

  return (
    <div className="mx-auto flex min-h-full max-w-6xl flex-col px-6 pb-36 lg:px-12">
      <TopBar />

      {/* Frequency header */}
      <section className="grid gap-8 py-10 md:grid-cols-[auto_1fr] md:items-end">
        <div>
          <div className="flex items-baseline gap-3">
            <Readout value={s.phase === 'tuning' || s.phase === 'loading' ? undefined : freq} className="text-6xl font-light sm:text-7xl" />
            <span className="font-mono text-sm text-dim">MHz</span>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-4 font-mono text-xs">
            <span role="status" className={`inline-flex items-center gap-2 uppercase tracking-[0.25em] ${tone === 'red' ? 'text-red' : tone === 'signal' ? 'text-signal' : 'text-amber'}`}>
              <span className={`size-1.5 rounded-full ${tone === 'red' ? 'bg-red' : tone === 'signal' ? 'bg-signal' : 'animate-pulse bg-amber'}`} />
              {t.call.phase[s.phase]}
            </span>
            {c && <span className="text-dim">{c.participants.length}/{c.capacity} {t.call.seats}</span>}
            <button
              onClick={() => navigator.clipboard.writeText(callLink(callId)).then(() => flash(t.call.copied))}
              className="inline-flex items-center gap-1.5 text-dim hover:text-signal"
            >
              <Copy width={14} height={14} /> {t.call.copy}
            </button>
          </div>
        </div>
        <Band value={s.phase === 'tuning' || s.phase === 'loading' ? undefined : freq} tone={tone} />
      </section>

      {/* Rerouting banner (relay failover) */}
      <AnimatePresence>
        {s.phase === 'rerouting' && (
          <motion.div role="status" initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }}
            className="mb-6 border border-amber/40 bg-amber/5 px-4 py-3 font-mono text-sm text-amber">
            {t.call.reroute}
          </motion.div>
        )}
      </AnimatePresence>

      {closed ? (
        <motion.section initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="my-auto max-w-lg space-y-4 py-16">
          <h1 className="font-display text-3xl font-light text-red">{c?.status === 'FAILED' ? t.call.failedTitle : t.call.endedTitle}</h1>
          {s.reason && <p className="font-mono text-sm text-dim">{why(s.reason)}</p>}
          <div className="flex gap-3 pt-2">
            <button onClick={() => navigate('/')} className="border border-signal/60 px-5 py-2.5 font-display text-xs uppercase tracking-[0.2em] text-signal hover:bg-signal hover:text-bg">
              {t.call.back}
            </button>
            {s.phase === 'error' && (
              <button onClick={() => location.reload()} className="border border-line px-5 py-2.5 font-display text-xs uppercase tracking-[0.2em] text-dim hover:text-text">
                {t.call.retry}
              </button>
            )}
          </div>
        </motion.section>
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] gap-4" aria-label="Participantes">
          <AnimatePresence>
            {c && [me, ...others].filter((u) => u === me || c.participants.includes(u)).map((u) => (
              <ParticipantTile
                key={u}
                user={u}
                me={u === me}
                speaking={speaking.has(u)}
                muted={u === me ? muted : c.muted.includes(u)}
                deafened={u === me ? deafened : c.deafened.includes(u)}
                level={u === me ? level : undefined}
              />
            ))}
          </AnimatePresence>
        </ul>
      )}

      {needsGesture && !closed && (
        <button onClick={() => { voice.current?.resumeAudio(); setNeedsGesture(false) }}
          className="mx-auto mt-8 border border-amber/60 px-5 py-2.5 font-mono text-sm text-amber hover:bg-amber hover:text-bg">
          {t.call.enableAudio}
        </button>
      )}

      {/* Toast-like notice */}
      <AnimatePresence>
        {notice && (
          <motion.p role="status" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}
            className="fixed bottom-28 left-1/2 z-30 -translate-x-1/2 border border-line bg-surface px-4 py-2 font-mono text-xs text-text">
            {notice}
          </motion.p>
        )}
      </AnimatePresence>

      {/* Control bar */}
      {!closed && (
        <motion.nav
          aria-label="Controles da chamada"
          initial={{ y: 40, opacity: 0 }}
          animate={{ y: 0, opacity: 1 }}
          transition={{ delay: 0.2 }}
          className="fixed inset-x-0 bottom-0 z-20 border-t border-line bg-bg/85 backdrop-blur"
        >
          <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-center gap-2 px-4 py-3">
            <Control label={muted ? t.call.mute : t.call.unmute} pressed={muted} onClick={toggleMute} hotkey="M">
              {muted ? <MicOff /> : <Mic />}
            </Control>
            <Control label={deafened ? t.call.deafen : t.call.undeafen} pressed={deafened} onClick={toggleDeafen} hotkey="D">
              {deafened ? <HeadphonesOff /> : <Headphones />}
            </Control>
            <Control label={t.call.ptt} pressed={ptt} onClick={togglePtt}>
              <Radio className={ptt && talking ? 'text-signal' : undefined} />
            </Control>
            <span className="mx-1 hidden h-10 w-px bg-line sm:block" />
            <Control label={t.call.invite} onClick={() => setPanel('invite')} hotkey="I"><UserPlus /></Control>
            <Control label={t.call.settings} onClick={() => setPanel('settings')}><Sliders /></Control>
            <Control label={t.call.ops} onClick={() => setPanel('ops')} hotkey="O"><Activity /></Control>
            <span className="mx-1 hidden h-10 w-px bg-line sm:block" />
            <Control label={t.call.leave} onClick={leave} danger><PhoneOff /></Control>
          </div>
          {ptt && <p className="pb-2 text-center font-mono text-[10px] uppercase tracking-[0.2em] text-dim">{t.call.pttHint}</p>}
        </motion.nav>
      )}

      <Drawer open={panel === 'invite'} title={t.call.invite} onClose={() => setPanel(null)}>
        <InviteInput values={[]} onChange={() => {}} exclude={me} onAdd={invite} />
      </Drawer>
      <Drawer open={panel === 'settings'} title={t.settings.title} onClose={() => setPanel(null)}>
        <SettingsPanel prefs={prefs} onChange={changePrefs} level={level} pttKey="Espaço" />
      </Drawer>
      <Drawer open={panel === 'ops'} title={t.ops.title} onClose={() => setPanel(null)}>
        <OpsPanel state={s} stats={stats} conn={conn} />
      </Drawer>
    </div>
  )
}
