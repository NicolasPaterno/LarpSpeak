// Radio visuals shared by every screen: CRT backdrop, tuning band, frequency readout.
import { useEffect, useState } from 'react'
import { motion, useReducedMotion } from 'motion/react'

export function Backdrop() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <div className="absolute -left-1/4 -top-1/3 h-[80vh] w-[80vw] rounded-full bg-signal/[0.05] blur-3xl" />
      <div className="absolute -bottom-1/3 right-0 h-[60vh] w-[50vw] rounded-full bg-blue/[0.04] blur-3xl" />
      <div className="scanlines absolute inset-0" />
      <div className="grain absolute inset-0" />
    </div>
  )
}

type BandProps = {
  /** "147.320" locks the needle there; undefined sweeps the band. */
  value?: string
  tone?: 'signal' | 'amber' | 'red'
  className?: string
}

const toneClass = { signal: 'bg-signal shadow-signal', amber: 'bg-amber shadow-amber', red: 'bg-red shadow-red' }

/** Tuning band 100–200 MHz with a needle: sweeps while searching, snaps when locked. */
export function Band({ value, tone = 'signal', className = '' }: BandProps) {
  const reduce = useReducedMotion()
  const pos = value ? `${Number(value) - 100}%` : undefined
  return (
    <div aria-hidden className={`relative h-14 select-none ${className}`}>
      <div className="absolute inset-x-0 top-0 flex h-8 items-end justify-between">
        {Array.from({ length: 101 }, (_, i) => (
          <span
            key={i}
            className={`w-px ${i % 10 === 0 ? 'h-full bg-dim/70' : i % 5 === 0 ? 'h-1/2 bg-line' : 'h-1/4 bg-line'}`}
          />
        ))}
      </div>
      <div className="absolute inset-x-0 bottom-0 flex justify-between font-mono text-[10px] text-dim/70">
        {Array.from({ length: 11 }, (_, i) => (
          <span key={i} className="w-0 -translate-x-1/2 text-center">{100 + i * 10}</span>
        ))}
      </div>
      <motion.span
        className={`absolute -top-1 h-10 w-0.5 -translate-x-1/2 rounded-full shadow-[0_0_12px] ${toneClass[tone]}`}
        initial={{ left: '50%' }}
        animate={
          pos ? { left: pos } : reduce ? { left: '50%' } : { left: ['6%', '94%'] }
        }
        transition={
          pos
            ? { type: 'spring', stiffness: 120, damping: 14 }
            : { duration: 7, repeat: Infinity, repeatType: 'mirror', ease: 'easeInOut' }
        }
      />
    </div>
  )
}

/** Big frequency digits. While searching they flicker through random channels. */
export function Readout({ value, className = '' }: { value?: string; className?: string }) {
  const reduce = useReducedMotion()
  const [noise, setNoise] = useState('---.---')
  useEffect(() => {
    if (value || reduce) return
    const id = setInterval(() => setNoise((100 + Math.random() * 100).toFixed(3)), 90)
    return () => clearInterval(id)
  }, [value, reduce])
  const shown = value ?? noise
  return (
    <span className={`font-display tabular-nums ${value ? 'text-signal glow-signal' : 'text-text/40'} ${className}`}>
      {shown}
    </span>
  )
}
