// One tab per open repository. Every tab stays live in the background; a dot
// flags tabs with conflicts or failing CI so you notice without switching.
import type { KeyboardEvent, MouseEvent } from 'react'
import type { Tab } from '../model'
import { CloseIcon, PlusIcon } from './icons'

interface Props {
  tabs: Tab[]
  onSelect(path: string): void
  onClose(path: string): void
  onAdd(): void
}

const ATTENTION_LABEL: Record<string, string> = {
  conflicts: 'a pull request has conflicts',
  ci_failing: 'CI is failing on a pull request',
}

export function TabBar({ tabs, onSelect, onClose, onAdd }: Props) {
  const move = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
    const i = tabs.findIndex((t) => t.active)
    if (i < 0 || tabs.length < 2) return
    e.preventDefault()
    const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length]
    onSelect(next.path)
    requestAnimationFrame(() => document.querySelector<HTMLElement>('.tab[aria-selected="true"] .tab-hit')?.focus())
  }
  return (
    <nav className="tabbar" aria-label="Repositories">
      <div className="tabs" role="tablist" aria-orientation="horizontal" onKeyDown={move}>
        {tabs.map((t, i) => {
          const why = ATTENTION_LABEL[t.attention] ?? ''
          const title = `${t.slug || t.path}${why ? ` — ${why}` : ''}${i < 9 ? ` (${i + 1})` : ''}`
          return (
            <div
              key={t.path}
              className={`tab${t.active ? ' is-active' : ''}`}
              role="tab"
              aria-selected={t.active}
              onAuxClick={(e: MouseEvent) => {
                if (e.button === 1) {
                  e.preventDefault()
                  onClose(t.path)
                }
              }}
            >
              <button type="button" className="tab-hit" tabIndex={t.active ? 0 : -1} title={title} onClick={() => onSelect(t.path)}>
                {t.attention && <span className={`tab-dot st-${t.attention}`} aria-label={why} />}
                <span className="tab-name">{t.name}</span>
              </button>
              <button
                type="button"
                className="tab-close"
                tabIndex={-1}
                aria-label={`Close ${t.name}`}
                title="Close tab"
                onClick={() => onClose(t.path)}
              >
                <CloseIcon size={12} />
              </button>
            </div>
          )
        })}
      </div>
      <button
        type="button"
        className="icon-btn small tab-add"
        onClick={onAdd}
        aria-label="Open a repository in a new tab"
        title="Open a repository (o)"
      >
        <PlusIcon size={14} />
      </button>
    </nav>
  )
}
