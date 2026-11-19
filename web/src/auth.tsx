import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import * as client from './api.ts'

type Auth = {
  status: 'loading' | 'out' | 'in'
  userId: string | null
  login: (u: string, p: string) => Promise<void>
  register: (u: string, p: string) => Promise<void>
  logout: () => Promise<void>
}

const Ctx = createContext<Auth | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Auth['status']>('loading')
  const [userId, setUserId] = useState<string | null>(null)

  const signedIn = (s: { userId: string } | null) => {
    setUserId(s?.userId ?? null)
    setStatus(s ? 'in' : 'out')
  }

  useEffect(() => {
    client.onSessionLost(() => signedIn(null))
    // Restore the session from the refresh cookie; no cookie or revoked -> login screen.
    client.refresh().then(signedIn, () => signedIn(null))
  }, [])

  const auth: Auth = {
    status,
    userId,
    login: async (u, p) => signedIn(await client.login(u, p)),
    register: async (u, p) => {
      await client.register(u, p)
      signedIn(await client.login(u, p))
    },
    logout: async () => {
      await client.logout()
      signedIn(null)
    },
  }
  return <Ctx.Provider value={auth}>{children}</Ctx.Provider>
}

export function useAuth(): Auth {
  const a = useContext(Ctx)
  if (!a) throw new Error('useAuth outside AuthProvider')
  return a
}
