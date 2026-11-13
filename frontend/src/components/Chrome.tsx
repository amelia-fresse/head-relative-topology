// Small pieces around the map: banners, footer toggle, full-page states.
import { plural } from '../format'
import { AlertIcon, CloseIcon, EyeIcon, EyeOffIcon, FolderIcon, InfoIcon } from './icons'

export interface Notice {
  id: string
  tone: 'warning' | 'info' | 'error'
  text: string
}

export function Banners({ notices, onDismiss }: { notices: Notice[]; onDismiss(id: string): void }) {
  if (notices.length === 0) return null
  return (
    <div className="banners" role="status" aria-live="polite">
      {notices.map((n) => (
        <div key={n.id} className={`banner banner-${n.tone}`}>
          <span className="banner-icon">{n.tone === 'info' ? <InfoIcon size={14} /> : <AlertIcon size={14} />}</span>
          <span className="banner-text">{n.text}</span>
          <button type="button" className="icon-btn small" onClick={() => onDismiss(n.id)} aria-label="Dismiss" title="Dismiss">
            <CloseIcon size={14} />
          </button>
        </div>
      ))}
    </div>
  )
}

export function HiddenToggle({ count, shown, onToggle }: { count: number; shown: boolean; onToggle(): void }) {
  if (count === 0) return null
  return (
    <footer className="footer">
      <button type="button" className="text-btn" aria-pressed={shown} onClick={onToggle} title="Show or hide hidden branches (h)">
        {shown ? <EyeIcon size={14} /> : <EyeOffIcon size={14} />}
        <span>
          {plural(count, 'hidden branch', 'hidden branches')}
          <span className="muted"> · {shown ? 'shown' : 'not shown'}</span>
        </span>
      </button>
    </footer>
  )
}

export function Loading() {
  return (
    <div className="state" role="status" aria-live="polite">
      <div className="spinner" aria-hidden="true" />
      <p>Reading the repository…</p>
    </div>
  )
}

export function ErrorState({ message, onRetry, onPick }: { message: string; onRetry(): void; onPick?: () => void }) {
  return (
    <div className="state" role="alert">
      <span className="state-icon error">
        <AlertIcon size={22} />
      </span>
      <h2>Couldn’t load the map</h2>
      <p className="state-detail">{message}</p>
      <div className="state-actions">
        <button type="button" className="btn primary" onClick={onRetry}>
          Try again
        </button>
        {onPick && (
          <button type="button" className="btn" onClick={onPick}>
            Open another repository
          </button>
        )}
      </div>
    </div>
  )
}

export function NoRepo({ onPick, busy }: { onPick(): void; busy: boolean }) {
  return (
    <div className="state">
      <span className="state-icon">
        <FolderIcon size={22} />
      </span>
      <h2>No repository open</h2>
      <p>Pick a Git repository to see where its branches sit relative to main.</p>
      <div className="state-actions">
        <button type="button" className="btn primary" onClick={onPick} disabled={busy}>
          <FolderIcon size={14} />
          Open a repository
        </button>
      </div>
    </div>
  )
}

export function EmptyMap({ hiddenCount }: { hiddenCount: number }) {
  return (
    <div className="empty">
      {hiddenCount > 0 ? (
        <p>
          Every branch is hidden right now. Use the toggle below to show {hiddenCount === 1 ? 'it' : `all ${hiddenCount}`}.
        </p>
      ) : (
        <p>
          No branches besides main yet. When you or an agent create one, it shows up here.
        </p>
      )}
    </div>
  )
}
