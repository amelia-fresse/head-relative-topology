import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'

export interface MenuItem {
  label: string
  icon?: ReactNode
  disabled?: boolean
  onSelect: () => void
}

export interface MenuState {
  x: number
  y: number
  label: string
  items: MenuItem[]
  returnFocus: HTMLElement | null
}

const MARGIN = 6

/** Small custom context menu: arrow keys, Home/End, Enter, Escape; closes on outside click, scroll or blur. */
export function ContextMenu({ menu, onClose }: { menu: MenuState; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)

  const close = (restore = true) => {
    onClose()
    if (restore && menu.returnFocus?.isConnected) menu.returnFocus.focus({ preventScroll: true })
  }

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const w = el.offsetWidth
    const h = el.offsetHeight
    const left = Math.max(MARGIN, Math.min(menu.x, window.innerWidth - w - MARGIN))
    const top = menu.y + h > window.innerHeight - MARGIN ? Math.max(MARGIN, menu.y - h) : menu.y
    setPos({ left, top })
    const first = el.querySelector<HTMLButtonElement>('button:not([disabled])')
    first?.focus({ preventScroll: true })
  }, [menu])

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close(false)
    }
    const onScroll = () => close(false)
    const onBlur = () => close(false)
    document.addEventListener('mousedown', onDown, true)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('blur', onBlur)
    window.addEventListener('resize', onBlur)
    return () => {
      document.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('blur', onBlur)
      window.removeEventListener('resize', onBlur)
    }
  }, [menu])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(ref.current?.querySelectorAll<HTMLButtonElement>('button:not([disabled])') ?? [])
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement)
    const go = (n: number) => buttons[(n + buttons.length) % buttons.length]?.focus()
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        go(i + 1)
        break
      case 'ArrowUp':
        e.preventDefault()
        go(i - 1)
        break
      case 'Home':
        e.preventDefault()
        go(0)
        break
      case 'End':
        e.preventDefault()
        go(buttons.length - 1)
        break
      case 'Escape':
        e.preventDefault()
        e.stopPropagation()
        close()
        break
      case 'Tab':
        e.preventDefault()
        close()
        break
    }
  }

  return (
    <div
      ref={ref}
      role="menu"
      aria-label={menu.label}
      className="menu"
      style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0, visibility: 'hidden' }}
      onKeyDown={onKeyDown}
      onContextMenu={(e) => e.preventDefault()}
    >
      {menu.items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          className="menu-item"
          disabled={item.disabled}
          onClick={() => {
            close()
            item.onSelect()
          }}
        >
          <span className="menu-icon" aria-hidden="true">
            {item.icon}
          </span>
          {item.label}
        </button>
      ))}
    </div>
  )
}
