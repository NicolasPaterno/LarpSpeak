import { useEffect, useId, useState, type ReactNode } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { HeadphonesOff, MicOff, X } from './Icons.tsx'
import type { AudioPrefs, Stats } from '../voice.ts'
import type { LogEntry, State } from '../call-state.ts'
import { t } from '../strings.ts'

// ---------- participant tile ----------

export function ParticipantTile({ user, me, speaking, muted, deafened, level }: {
  user: string
  me: boolean
  speaking: boolean
  muted: boolean
  deafened: boolean
  level?: number
}) {
  const reduce = useReducedMotion()
  const status = [speaking && t.call.speaking, muted && t.call.muted, deafened && t.call.deafened].filter(Boolean).join(', ')
  return (
    <motion.li
      layout
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.94 }}
      transition={{ type: 'spring', stiffness: 260, damping: 26 }}
      aria-label={`${user}${me ? ` (${t.call.you})` : ''}${status ? ': ' + status : ''}`}
      className={`relative flex aspect-[4/3] flex-col items-center justify-center gap-4 overflow-hidden border bg-surface/70 transition-colors ${speaking ? 'border-signal/70' : 'border-line'}`}
    >
      <div className="relative grid size-20 place-items-center">
        <AnimatePresence>
          {speaking && !reduce &&
            [0, 1, 2].map((i) => (
              <motion.span
                key={i}
                aria-hidden
                className="absolute inset-0 rounded-full border border-signal"
                initial={{ scale: 1, opacity: 0.7 }}
                animate={{ scale: 2.1, opacity: 0 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 1.6, repeat: Infinity, delay: i * 0.53, ease: 'easeOut' }}
              />
            ))}
        </AnimatePresence>
        <span
          className={`relative grid size-20 place-items-center rounded-full border font-display text-2xl uppercase tracking-wider transition-colors ${speaking ? 'border-signal bg-signal/10 text-signal glow-signal' : 'border-line bg-raised text-text/80'}`}
        >
          {user.slice(0, 2)}
        </span>
      </div>
      <div className="flex items-center gap-2 font-mono text-sm">
        <span className={me ? 'text-signal' : 'text-text'}>{user}</span>
        {me && <span className="border border-line px-1.5 text-[10px] uppercase tracking-widest text-dim">{t.call.you}</span>}
      </div>
      <div className="absolute right-3 top-3 flex gap-1.5 text-red">
        {muted && <MicOff width={16} height={16} />}
        {deafened && <HeadphonesOff width={16} height={16} />}
      </div>
      {me && level !== undefined && (
        <div aria-hidden className="absolute inset-x-0 bottom-0 h-0.5 bg-line">
          <div className="h-full bg-signal transition-[width] duration-75" style={{ width: `${Math.round(level * 100)}%` }} />
        </div>
      )}
    </motion.li>
  )
}

// ---------- control button ----------

export function Control({ label, pressed, onClick, danger, hotkey, children }: {
  label: string
  pressed?: boolean
  onClick: () => void
  danger?: boolean
  hotkey?: string
  children: ReactNode
}) {
  const tone = danger
    ? 'border-red/50 text-red hover:bg-red hover:text-bg'
    : pressed
      ? 'border-red/60 bg-red/10 text-red'
      : 'border-line text-text hover:border-signal hover:text-signal'
  return (
    <button
      onClick={onClick}
      aria-pressed={pressed}
      aria-keyshortcuts={hotkey}
      title={hotkey ? `${label} (${hotkey})` : label}
      className={`group flex min-w-16 flex-col items-center gap-1 border px-3 py-2 transition-colors ${tone}`}
    >
      {children}
      <span className="font-mono text-[10px] uppercase tracking-[0.15em]">{label}</span>
    </button>
  )
}

// ---------- side drawer ----------

export function Drawer({ open, title, onClose, children }: { open: boolean; title: string; onClose: () => void; children: ReactNode }) {
  const id = useId()
  return (
    <AnimatePresence>
      {open && (
        <motion.aside
          role="dialog"
          aria-labelledby={id}
          initial={{ x: '100%' }}
          animate={{ x: 0 }}
          exit={{ x: '100%' }}
          transition={{ type: 'spring', stiffness: 300, damping: 34 }}
          className="fixed inset-y-0 right-0 z-40 flex w-[min(26rem,100vw)] flex-col border-l border-line bg-surface/95 backdrop-blur"
        >
          <header className="flex items-center justify-between border-b border-line px-5 py-4">
            <h2 id={id} className="font-mono text-[11px] uppercase tracking-[0.3em] text-dim">{title}</h2>
            <button onClick={onClose} aria-label="Fechar" className="text-dim hover:text-text"><X /></button>
          </header>
          <div className="flex-1 overflow-y-auto px-5 py-5">{children}</div>
        </motion.aside>
      )}
    </AnimatePresence>
  )
}

// ---------- settings ----------

export function SettingsPanel({ prefs, onChange, level, pttKey }: {
  prefs: AudioPrefs
  onChange: (p: AudioPrefs) => void
  level: number
  pttKey: string
}) {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  useEffect(() => {
    const load = () => navigator.mediaDevices.enumerateDevices().then(setDevices).catch(() => {})
    load()
    navigator.mediaDevices.addEventListener('devicechange', load)
    return () => navigator.mediaDevices.removeEventListener('devicechange', load)
  }, [])
  const sinkSupported = 'setSinkId' in HTMLMediaElement.prototype
  const select = 'w-full border border-line bg-bg px-3 py-2 font-mono text-sm text-text outline-none focus:border-signal'
  const label = 'mb-2 block font-mono text-[11px] uppercase tracking-[0.3em] text-dim'
  const list = (kind: MediaDeviceKind) => devices.filter((d) => d.kind === kind && d.deviceId !== 'default')
  return (
    <div className="space-y-7">
      <div>
        <label className={label} htmlFor="in">{t.settings.input}</label>
        <select id="in" className={select} value={prefs.inputId ?? ''} onChange={(e) => onChange({ ...prefs, inputId: e.target.value || undefined })}>
          <option value="">{t.settings.default}</option>
          {list('audioinput').map((d) => <option key={d.deviceId} value={d.deviceId}>{d.label || d.deviceId.slice(0, 8)}</option>)}
        </select>
      </div>
      <div>
        <span className={label}>{t.settings.level}</span>
        <div className="flex h-3 gap-0.5" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(level * 100)} aria-label={t.settings.level}>
          {Array.from({ length: 24 }, (_, i) => (
            <span key={i} className={`flex-1 transition-colors duration-75 ${i / 24 < level ? (i > 19 ? 'bg-red' : i > 15 ? 'bg-amber' : 'bg-signal') : 'bg-line'}`} />
          ))}
        </div>
      </div>
      <div>
        <label className={label} htmlFor="out">{t.settings.output}</label>
        {sinkSupported ? (
          <select id="out" className={select} value={prefs.outputId ?? ''} onChange={(e) => onChange({ ...prefs, outputId: e.target.value || undefined })}>
            <option value="">{t.settings.default}</option>
            {list('audiooutput').map((d) => <option key={d.deviceId} value={d.deviceId}>{d.label || d.deviceId.slice(0, 8)}</option>)}
          </select>
        ) : (
          <p className="text-sm text-dim">{t.settings.outputUnsupported}</p>
        )}
      </div>
      {([['echoCancel', t.settings.echo], ['noiseSuppress', t.settings.noise]] as const).map(([k, text]) => (
        <label key={k} className="flex cursor-pointer items-center justify-between text-sm">
          {text}
          <input type="checkbox" checked={prefs[k]} onChange={(e) => onChange({ ...prefs, [k]: e.target.checked })} className="size-4 accent-[var(--color-signal)]" />
        </label>
      ))}
      <p className="text-sm text-dim">{t.settings.pttKey}: <kbd className="border border-line px-1.5 font-mono text-text">{pttKey}</kbd></p>
    </div>
  )
}

// ---------- ops panel ----------

export function OpsPanel({ state, stats, conn }: { state: State; stats: Stats; conn: string }) {
  const c = state.call
  const row = (k: string, v: ReactNode) => (
    <div className="flex justify-between gap-4 border-b border-line/60 py-2">
      <dt className="text-dim">{k}</dt>
      <dd className="text-right text-text">{v ?? '—'}</dd>
    </div>
  )
  return (
    <div className="space-y-8 font-mono text-xs">
      <dl>
        {row(t.ops.status, <span className={c?.status === 'ACTIVE' ? 'text-signal' : c?.status === 'PENDING' ? 'text-amber' : 'text-red'}>{c?.status} · {state.phase}</span>)}
        {row(t.ops.seats, c && `${c.participants.length}/${c.capacity} (${c.kind})`)}
        {row(t.ops.relay, stats.remote ?? (c?.relayHost && `${c.relayHost}:${c.relayPort}`))}
        {row(t.ops.ice, conn)}
        {row(t.ops.rtt, stats.rttMs !== undefined && `${stats.rttMs} ms`)}
        {row(t.ops.jitter, stats.jitterMs !== undefined && `${stats.jitterMs} ms`)}
        {row(t.ops.loss, stats.lossPct !== undefined && `${stats.lossPct}%`)}
        {row(t.ops.bitrate, stats.sendKbps !== undefined && `${stats.sendKbps} kbps`)}
        {row('callId', <span className="break-all">{c?.callId}</span>)}
      </dl>
      <section>
        <h3 className="mb-2 uppercase tracking-[0.3em] text-dim">{t.ops.log}</h3>
        <ol className="space-y-1.5">
          {state.log.map((e: LogEntry, i) => (
            <li key={state.log.length - i} className="grid grid-cols-[auto_auto_1fr] gap-2">
              <time className="text-dim/70">{new Date(e.at).toLocaleTimeString('pt-BR')}</time>
              <span className={e.source === 'sse' ? 'text-blue' : e.source === 'rtc' ? 'text-signal' : 'text-amber'}>{e.type}</span>
              <span className="truncate text-dim" title={JSON.stringify(e.data)}>{e.data === undefined ? '' : JSON.stringify(e.data)}</span>
            </li>
          ))}
        </ol>
      </section>
    </div>
  )
}
