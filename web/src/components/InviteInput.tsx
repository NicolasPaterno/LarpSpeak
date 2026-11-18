import { useState, type KeyboardEvent } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { X } from './Icons.tsx'
import { t } from '../strings.ts'

const VALID = /^[a-z0-9_-]{3,32}$/

/** Username chips: Enter/comma/space adds, Backspace on empty removes the last. */
export function InviteInput({ values, onChange, exclude, onAdd }: {
  values: string[]
  onChange: (v: string[]) => void
  exclude?: string
  onAdd?: (u: string) => void
}) {
  const [text, setText] = useState('')
  const add = () => {
    const u = text.trim().toLowerCase()
    if (VALID.test(u) && u !== exclude && !values.includes(u)) {
      onChange([...values, u])
      onAdd?.(u)
    }
    setText('')
  }
  const key = (e: KeyboardEvent<HTMLInputElement>) => {
    if (['Enter', ',', ' '].includes(e.key)) {
      e.preventDefault()
      add()
    } else if (e.key === 'Backspace' && !text && values.length) {
      onChange(values.slice(0, -1))
    }
  }
  return (
    <div className="flex min-h-11 flex-wrap items-center gap-2 border-b border-line py-1.5 focus-within:border-signal">
      <AnimatePresence initial={false}>
        {values.map((u) => (
          <motion.span
            key={u}
            layout
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.8 }}
            className="inline-flex items-center gap-1 border border-signal/30 bg-signal/5 py-0.5 pl-2 pr-1 font-mono text-xs text-signal"
          >
            {u}
            <button type="button" aria-label={`remover ${u}`} onClick={() => onChange(values.filter((x) => x !== u))} className="text-signal/60 hover:text-red">
              <X width={14} height={14} />
            </button>
          </motion.span>
        ))}
      </AnimatePresence>
      <input
        aria-label={t.lobby.invite}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={key}
        onBlur={add}
        placeholder={values.length ? '' : t.lobby.invitePlaceholder}
        spellCheck={false}
        className="min-w-32 flex-1 border-0 bg-transparent py-1 font-mono text-sm outline-none placeholder:text-dim/50 focus-visible:outline-none"
      />
    </div>
  )
}
