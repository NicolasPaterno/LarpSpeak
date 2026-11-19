import { AnimatePresence, motion } from 'motion/react'
import { useLive } from '../live.tsx'
import { navigate } from '../nav.ts'
import { frequency } from '../frequency.ts'
import { t } from '../strings.ts'

/** Top-right ringing card for invitations, on every screen except the call itself. */
export function IncomingToast({ currentCall }: { currentCall: string | null }) {
  const live = useLive()
  const inv = live.invites.find((i) => i.callId !== currentCall)
  return (
    <div className="pointer-events-none fixed right-4 top-4 z-50 w-80 max-w-[calc(100vw-2rem)]" aria-live="assertive">
      <AnimatePresence>
        {inv && (
          <motion.div
            key={inv.callId}
            role="alertdialog"
            aria-label={`${t.lobby.incoming}: ${inv.from}`}
            initial={{ opacity: 0, y: -16, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -16, scale: 0.97 }}
            className="pointer-events-auto relative overflow-hidden border border-signal/40 bg-surface/95 p-4 shadow-[0_0_40px] shadow-signal/10 backdrop-blur"
          >
            <span className="absolute left-4 top-5 size-3 animate-ping rounded-full bg-signal/60" aria-hidden />
            <span className="absolute left-4 top-5 size-3 rounded-full bg-signal" aria-hidden />
            <div className="pl-7">
              <p className="font-mono text-[11px] uppercase tracking-[0.3em] text-dim">{t.lobby.incoming}</p>
              <p className="mt-1 font-display text-2xl text-signal">{frequency(inv.callId)}</p>
              <p className="font-mono text-xs text-dim">{t.lobby.from} <span className="text-text">{inv.from}</span></p>
              <div className="mt-4 flex gap-2 font-display text-xs uppercase tracking-[0.15em]">
                <button onClick={() => live.dismiss(inv.callId)} className="flex-1 border border-line py-2 text-dim hover:border-red hover:text-red">
                  {t.lobby.decline}
                </button>
                <button
                  autoFocus
                  onClick={() => { live.dismiss(inv.callId); navigate('/c/' + inv.callId) }}
                  className="flex-1 border border-signal bg-signal py-2 text-bg hover:bg-signal/80"
                >
                  {t.lobby.accept}
                </button>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
