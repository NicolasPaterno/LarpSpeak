import { useId, useState, type FormEvent } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { useAuth } from '../auth.tsx'
import { ApiError } from '../api.ts'
import { Band, Readout } from '../components/Radio.tsx'
import { t } from '../strings.ts'

type Mode = 'login' | 'register'

function message(err: unknown, mode: Mode): string {
  if (!(err instanceof ApiError)) return t.auth.offline
  if (err.status === 401) return t.auth.invalid
  if (err.status === 409) return t.auth.taken
  if (err.status === 400) return t.auth.rules
  return t.auth.offline
}

export function AuthScreen() {
  const auth = useAuth()
  const [mode, setMode] = useState<Mode>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await (mode === 'login' ? auth.login : auth.register)(username, password)
    } catch (err) {
      setError(message(err, mode))
      setBusy(false)
    }
  }

  return (
    <main className="mx-auto grid min-h-full max-w-6xl items-center gap-16 px-6 py-12 lg:grid-cols-[1.2fr_1fr] lg:px-12">
      <motion.section
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.7, ease: 'easeOut' }}
        className="space-y-10"
      >
        <p className="font-mono text-xs uppercase tracking-[0.4em] text-dim">{t.brand}</p>
        <div className="space-y-3">
          <h1 className="sr-only">{t.brand}</h1>
          <Readout className="block text-6xl font-light sm:text-8xl" />
          <p className="flex items-center gap-2 font-mono text-xs uppercase tracking-[0.25em] text-amber">
            <span className="size-1.5 animate-pulse rounded-full bg-amber" />
            MHz · {t.scanning}
          </p>
        </div>
        <p className="max-w-sm text-lg text-dim">{t.tagline}</p>
        <Band className="max-w-xl" tone="amber" />
      </motion.section>

      <motion.section
        initial={{ opacity: 0, x: 16 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: 0.7, delay: 0.15, ease: 'easeOut' }}
        className="border-l border-line pl-8 sm:pl-12"
      >
        <div role="tablist" className="mb-10 flex gap-8">
          {(['login', 'register'] as const).map((m) => (
            <button
              key={m}
              role="tab"
              aria-selected={mode === m}
              onClick={() => { setMode(m); setError(null) }}
              className={`relative pb-2 font-display text-sm uppercase tracking-[0.2em] transition-colors ${mode === m ? 'text-text' : 'text-dim hover:text-text'}`}
            >
              {t.auth[m]}
              {mode === m && (
                <motion.span layoutId="tab-underline" className="absolute inset-x-0 -bottom-px h-px bg-signal shadow-[0_0_8px] shadow-signal" />
              )}
            </button>
          ))}
        </div>

        <form onSubmit={submit} className="space-y-7" noValidate>
          <Field
            label={t.auth.username}
            hint={mode === 'register' ? t.auth.usernameHint : undefined}
            value={username}
            onChange={(v) => setUsername(v.toLowerCase().trim())}
            autoComplete="username"
            pattern="[a-z0-9_\-]{3,32}"
          />
          <Field
            label={t.auth.password}
            hint={mode === 'register' ? t.auth.passwordHint : undefined}
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />

          <AnimatePresence>
            {error && (
              <motion.p
                role="alert"
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                exit={{ opacity: 0, height: 0 }}
                className="font-mono text-xs text-red"
              >
                {error}
              </motion.p>
            )}
          </AnimatePresence>

          <button
            type="submit"
            disabled={busy || !username || !password}
            className="group relative w-full overflow-hidden border border-signal/60 px-6 py-3.5 font-display text-sm uppercase tracking-[0.25em] text-signal transition-colors hover:bg-signal hover:text-bg disabled:cursor-not-allowed disabled:border-line disabled:text-dim disabled:hover:bg-transparent"
          >
            {busy ? (
              <span className="inline-flex items-center gap-3">
                <span className="size-1.5 animate-ping rounded-full bg-current" />
                {t.auth.busy}
              </span>
            ) : mode === 'login' ? t.auth.submitLogin : t.auth.submitRegister}
          </button>
        </form>
      </motion.section>
    </main>
  )
}

type FieldProps = {
  label: string
  hint?: string
  value: string
  onChange: (v: string) => void
  type?: string
  autoComplete?: string
  pattern?: string
}

function Field({ label, hint, value, onChange, type = 'text', autoComplete, pattern }: FieldProps) {
  const id = useId()
  return (
    <div>
      <label htmlFor={id} className="mb-2 block font-mono text-[11px] uppercase tracking-[0.3em] text-dim">
        {label}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        pattern={pattern}
        spellCheck={false}
        aria-describedby={hint ? id + '-hint' : undefined}
        className="w-full border-0 border-b border-line bg-transparent px-0 py-2 font-mono text-lg text-text outline-none transition-colors placeholder:text-dim/40 focus:border-signal focus-visible:outline-none"
      />
      {hint && <p id={id + '-hint'} className="mt-1.5 text-xs text-dim">{hint}</p>}
    </div>
  )
}
