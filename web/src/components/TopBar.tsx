import { AnimatePresence, motion } from 'motion/react'
import { useAuth } from '../auth.tsx'
import { useLive } from '../live.tsx'
import { navigate } from '../nav.ts'
import { t } from '../strings.ts'

export function TopBar() {
  const auth = useAuth()
  const live = useLive()
  return (
    <header className="flex items-center justify-between gap-4 border-b border-line py-4">
      <button onClick={() => navigate('/')} className="font-mono text-xs uppercase tracking-[0.4em] text-dim hover:text-text">
        {t.brand}
      </button>
      <div className="flex items-center gap-5 font-mono text-xs">
        <AnimatePresence>
          {!live.connected && (
            <motion.span
              role="status"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="hidden items-center gap-2 text-amber sm:flex"
            >
              <span className="size-1.5 animate-pulse rounded-full bg-amber" />
              {t.live.reconnecting}
            </motion.span>
          )}
        </AnimatePresence>
        <span className="text-dim">
          {t.session.signedAs} <span className="text-signal">{auth.userId}</span>
        </span>
        <button onClick={auth.logout} className="uppercase tracking-[0.2em] text-dim hover:text-red">
          {t.session.logout}
        </button>
      </div>
    </header>
  )
}
