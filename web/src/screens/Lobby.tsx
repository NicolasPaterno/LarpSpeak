import { useState, type FormEvent } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { api, ApiError } from '../api.ts'
import { useAuth } from '../auth.tsx'
import { useLive } from '../live.tsx'
import { navigate, parseCallId } from '../nav.ts'
import { frequency } from '../frequency.ts'
import { Band } from '../components/Radio.tsx'
import { TopBar } from '../components/TopBar.tsx'
import { InviteInput } from '../components/InviteInput.tsx'
import { t } from '../strings.ts'

const MAX = 10

export function Lobby() {
  const auth = useAuth()
  const live = useLive()
  const [seats, setSeats] = useState(MAX)
  const [direct, setDirect] = useState(false)
  const [invitees, setInvitees] = useState<string[]>([])
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function open(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      // Direct (1:1) = one invitee and no explicit seats; the server makes it a group if a 3rd person comes.
      const body = direct && invitees.length === 1 ? { calleeIds: invitees } : { calleeIds: invitees, maxParticipants: seats }
      const { callId } = await api<{ callId: string }>('/calls', { method: 'POST', body })
      navigate('/c/' + callId)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t.auth.offline)
      setBusy(false)
    }
  }

  function tune(e: FormEvent) {
    e.preventDefault()
    const id = parseCallId(code)
    if (id) navigate('/c/' + id)
    else setError(t.lobby.badId)
  }

  return (
    <div className="mx-auto max-w-6xl px-6 pb-16 lg:px-12">
      <TopBar />
      <div className="grid gap-12 pt-12 lg:grid-cols-[1.15fr_1fr]">
        <motion.form
          onSubmit={open}
          initial={{ opacity: 0, y: 14 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5 }}
          className="space-y-8"
        >
          <header>
            <h1 className="font-display text-4xl font-light tracking-wide">{t.lobby.openTitle}</h1>
            <p className="mt-2 text-dim">{t.lobby.openLead}</p>
          </header>

          <div className={direct ? 'opacity-40' : ''}>
            <label htmlFor="seats" className="mb-3 flex items-baseline justify-between font-mono text-[11px] uppercase tracking-[0.3em] text-dim">
              {t.lobby.seats}
              <span className="font-display text-2xl tracking-normal text-signal">{seats}</span>
            </label>
            <input
              id="seats"
              type="range"
              min={2}
              max={MAX}
              value={seats}
              disabled={direct}
              onChange={(e) => setSeats(Number(e.target.value))}
              className="w-full accent-[var(--color-signal)]"
            />
            <div className="mt-1 flex justify-between font-mono text-[10px] text-dim/60">
              {Array.from({ length: MAX - 1 }, (_, i) => <span key={i}>{i + 2}</span>)}
            </div>
          </div>

          <div>
            <span className="mb-2 block font-mono text-[11px] uppercase tracking-[0.3em] text-dim">{t.lobby.invite}</span>
            <InviteInput values={invitees} onChange={setInvitees} exclude={auth.userId ?? ''} />
            {invitees.length === 1 && (
              <label className="mt-3 flex cursor-pointer items-start gap-2 text-sm text-dim">
                <input type="checkbox" checked={direct} onChange={(e) => setDirect(e.target.checked)} className="mt-1 accent-[var(--color-signal)]" />
                {t.lobby.direct}
              </label>
            )}
          </div>

          <button
            disabled={busy}
            className="w-full border border-signal/60 px-6 py-3.5 font-display text-sm uppercase tracking-[0.25em] text-signal transition-colors hover:bg-signal hover:text-bg disabled:border-line disabled:text-dim"
          >
            {busy ? t.auth.busy : t.lobby.open}
          </button>
          <AnimatePresence>
            {error && (
              <motion.p role="alert" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="font-mono text-xs text-red">
                {error}
              </motion.p>
            )}
          </AnimatePresence>
        </motion.form>

        <motion.div
          initial={{ opacity: 0, y: 14 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5, delay: 0.1 }}
          className="space-y-12 border-line lg:border-l lg:pl-12"
        >
          <form onSubmit={tune} className="space-y-4">
            <header>
              <h2 className="font-display text-2xl font-light tracking-wide">{t.lobby.tuneTitle}</h2>
              <p className="mt-1 text-sm text-dim">{t.lobby.tuneLead}</p>
            </header>
            <div className="flex gap-3">
              <input
                aria-label={t.lobby.tunePlaceholder}
                value={code}
                onChange={(e) => { setCode(e.target.value); setError(null) }}
                placeholder={t.lobby.tunePlaceholder}
                spellCheck={false}
                className="min-w-0 flex-1 border-0 border-b border-line bg-transparent py-2 font-mono text-sm outline-none placeholder:text-dim/50 focus:border-signal focus-visible:outline-none"
              />
              <button className="border border-line px-4 font-display text-xs uppercase tracking-[0.2em] text-text hover:border-signal hover:text-signal">
                {t.lobby.tune}
              </button>
            </div>
          </form>

          <section aria-labelledby="incoming">
            <h2 id="incoming" className="mb-4 font-mono text-[11px] uppercase tracking-[0.3em] text-dim">{t.lobby.incoming}</h2>
            {live.invites.length === 0 ? (
              <div className="space-y-5 py-4">
                <Band tone="amber" className="opacity-60" />
                <p className="text-sm text-dim">{t.lobby.empty}</p>
              </div>
            ) : (
              <ul className="space-y-3">
                <AnimatePresence initial={false}>
                  {live.invites.map((inv) => (
                    <motion.li
                      key={inv.callId}
                      layout
                      initial={{ opacity: 0, x: -12 }}
                      animate={{ opacity: 1, x: 0 }}
                      exit={{ opacity: 0, x: 12 }}
                      className="flex items-center justify-between gap-4 border border-line bg-surface/60 px-4 py-3"
                    >
                      <div>
                        <p className="font-display text-xl text-signal">{frequency(inv.callId)}</p>
                        <p className="font-mono text-xs text-dim">{t.lobby.from} {inv.from}</p>
                      </div>
                      <div className="flex gap-2 font-display text-xs uppercase tracking-[0.15em]">
                        <button onClick={() => live.dismiss(inv.callId)} className="px-3 py-2 text-dim hover:text-red">{t.lobby.decline}</button>
                        <button onClick={() => { live.dismiss(inv.callId); navigate('/c/' + inv.callId) }} className="border border-signal/60 px-3 py-2 text-signal hover:bg-signal hover:text-bg">
                          {t.lobby.accept}
                        </button>
                      </div>
                    </motion.li>
                  ))}
                </AnimatePresence>
              </ul>
            )}
          </section>
        </motion.div>
      </div>
    </div>
  )
}
