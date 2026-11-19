import { useEffect, useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { useAuth } from './auth.tsx'
import { LiveProvider } from './live.tsx'
import { parseCallId } from './nav.ts'
import { AuthScreen } from './screens/Auth.tsx'
import { Lobby } from './screens/Lobby.tsx'
import { Call } from './screens/Call.tsx'
import { IncomingToast } from './components/IncomingToast.tsx'
import { Backdrop, Readout } from './components/Radio.tsx'
import { t } from './strings.ts'

function usePath() {
  const [path, setPath] = useState(location.pathname)
  useEffect(() => {
    const on = () => setPath(location.pathname)
    addEventListener('popstate', on)
    return () => removeEventListener('popstate', on)
  }, [])
  return path
}

export function App() {
  const auth = useAuth()
  const path = usePath()
  const callId = path.startsWith('/c/') ? parseCallId(path) : null
  const screen = auth.status === 'in' ? (callId ? 'call:' + callId : 'lobby') : auth.status

  return (
    <>
      <Backdrop />
      <AnimatePresence mode="wait">
        <motion.div
          key={screen}
          className="min-h-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, filter: 'blur(6px)' }}
          transition={{ duration: 0.3 }}
        >
          {auth.status === 'loading' && (
            <div className="grid min-h-full place-items-center" role="status">
              <div className="text-center">
                <Readout className="block text-5xl font-light" />
                <p className="mt-3 font-mono text-xs uppercase tracking-[0.3em] text-dim">{t.session.loading}</p>
              </div>
            </div>
          )}
          {auth.status === 'out' && <AuthScreen />}
          {auth.status === 'in' && (
            <LiveProvider>
              {callId ? <Call key={callId} callId={callId} /> : <Lobby />}
              {callId && <IncomingToast currentCall={callId} />}
            </LiveProvider>
          )}
        </motion.div>
      </AnimatePresence>
    </>
  )
}
