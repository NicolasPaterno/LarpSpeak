// Inline icons (stroke = currentColor). Decorative: buttons carry text labels.
import type { SVGProps } from 'react'

const base = (p: SVGProps<SVGSVGElement>) => ({
  width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.7,
  strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true, ...p,
})

export const Mic = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><rect x="9" y="3" width="6" height="11" rx="3" /><path d="M5 11a7 7 0 0 0 14 0M12 18v3" /></svg>
)
export const MicOff = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M15 9.3V6a3 3 0 0 0-5.7-1.3M9 9v2a3 3 0 0 0 4.9 2.3M19 11a7 7 0 0 1-1.1 3.8M5 11a7 7 0 0 0 10.6 6M12 18v3M3 3l18 18" /></svg>
)
export const Headphones = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M4 15v-3a8 8 0 0 1 16 0v3" /><rect x="3" y="14" width="4" height="6" rx="1.5" /><rect x="17" y="14" width="4" height="6" rx="1.5" /></svg>
)
export const HeadphonesOff = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M4 15v-3a8 8 0 0 1 13.7-5.7M20 12v3" /><rect x="3" y="14" width="4" height="6" rx="1.5" /><path d="M17 17v1.5A1.5 1.5 0 0 0 18.5 20h1M3 3l18 18" /></svg>
)
export const Radio = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><circle cx="12" cy="12" r="2" /><path d="M8.5 8.5a5 5 0 0 0 0 7M15.5 8.5a5 5 0 0 1 0 7M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8" /></svg>
)
export const UserPlus = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><circle cx="10" cy="8" r="4" /><path d="M3 20a7 7 0 0 1 12.5-4.3M19 14v6M16 17h6" /></svg>
)
export const Sliders = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0" /><circle cx="16" cy="6" r="2" /><circle cx="10" cy="12" r="2" /><circle cx="18" cy="18" r="2" /></svg>
)
export const Activity = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M3 12h4l3-8 4 16 3-8h4" /></svg>
)
export const PhoneOff = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M3.5 13.5c4.7-4.7 12.3-4.7 17 0l-1.8 2.6a1.5 1.5 0 0 1-1.9.5l-2.3-1a1.5 1.5 0 0 1-.9-1.4V13a10 10 0 0 0-3.2 0v1.2a1.5 1.5 0 0 1-.9 1.4l-2.3 1a1.5 1.5 0 0 1-1.9-.5z" /></svg>
)
export const Copy = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>
)
export const X = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}><path d="M6 6l12 12M18 6L6 18" /></svg>
)
