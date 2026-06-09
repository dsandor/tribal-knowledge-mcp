// Animated counter — pure CSS transitions, no external deps.
import { useEffect, useRef, useState } from 'react'
import './Counter.css'

interface CounterProps {
  value: number
  fontSize?: number
  textColor?: string
  fontWeight?: number | string
}

export default function Counter({ value, fontSize = 48, textColor = 'inherit', fontWeight = 700 }: CounterProps) {
  const [displayed, setDisplayed] = useState(value)
  const [phase, setPhase] = useState<'idle' | 'out' | 'in'>('idle')
  const pendingRef = useRef(value)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (value === displayed && phase === 'idle') return
    pendingRef.current = value

    if (timerRef.current) clearTimeout(timerRef.current)

    // Slide out the old number
    setPhase('out')
    timerRef.current = setTimeout(() => {
      setDisplayed(pendingRef.current)
      setPhase('in')
      timerRef.current = setTimeout(() => setPhase('idle'), 180)
    }, 160)

    return () => { if (timerRef.current) clearTimeout(timerRef.current) }
  }, [value]) // eslint-disable-line react-hooks/exhaustive-deps

  const transform = phase === 'out' ? 'translateY(-10px)' : phase === 'in' ? 'translateY(4px)' : 'translateY(0)'
  const opacity = phase === 'idle' ? 1 : 0

  return (
    <span
      className="counter-container"
      style={{
        display: 'inline-block',
        fontSize,
        fontWeight,
        color: textColor,
        fontVariantNumeric: 'tabular-nums',
        transition: 'opacity 0.16s ease, transform 0.16s ease',
        opacity,
        transform,
        lineHeight: 1,
      }}
    >
      {displayed.toLocaleString()}
    </span>
  )
}
