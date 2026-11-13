import { useEffect, useRef, useState } from 'react'

/** Current time, re-rendering every `intervalMs` so relative times stay fresh. */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
  return now
}

/** A ref that always holds the latest value (for stable callbacks). */
export function useLatest<T>(value: T) {
  const ref = useRef(value)
  ref.current = value
  return ref
}

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
}

/** Single-key shortcuts (no modifiers), ignored while typing or when `enabled` is false. */
export function useShortcuts(map: Record<string, () => void>, enabled: boolean) {
  const latest = useLatest(map)
  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.repeat) return
      if (isTyping(e.target)) return
      const fn = latest.current[e.key.toLowerCase()]
      if (fn && !e.shiftKey) {
        e.preventDefault()
        fn()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [enabled, latest])
}
