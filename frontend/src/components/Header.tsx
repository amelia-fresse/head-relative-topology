import { baseName, relTime } from '../format'
import type { Settings, Snapshot } from '../model'
import { CompactIcon, MergeIcon, RefreshIcon, ThemeIcon } from './icons'

interface Props {
  snapshot: Snapshot | null
  settings: Settings
  now: number
  refreshing: boolean
  onRefresh(): void
  onToggleCompact(): void
  onToggleMerged(): void
  onCycleTheme(): void
}

const THEME_LABEL: Record<string, string> = { default: 'Default', babydoll: 'Babydoll', dolly: 'Dolly' }

export function Header({ snapshot, settings, now, refreshing, onRefresh, onToggleCompact, onToggleMerged, onCycleTheme }: Props) {
  const repo = snapshot?.repo
  const repoName = repo ? repo.slug || (repo.path ? baseName(repo.path) : '') : ''
  const fetched = snapshot ? relTime(snapshot.fetchedAt, now) : null
  const themeLabel = THEME_LABEL[settings.theme] ?? settings.theme
  return (
    <header className="header">
      <div className="header-text">
        <div className="header-top">
          <span className="app-name" title="HEAD-Relative Topology">
            hrt
          </span>
          {repoName && (
            <span className="repo" title={repo?.path}>
              {repoName}
            </span>
          )}
        </div>
        {snapshot && repo?.path && <div className="fetched">{fetched ? `fetched ${fetched}` : 'not fetched yet'}</div>}
      </div>
      <div className="header-actions">
        <button
          type="button"
          className={`icon-btn${refreshing ? ' is-busy' : ''}`}
          onClick={onRefresh}
          disabled={!snapshot || !repo?.path}
          aria-label="Refresh (fetch from remote)"
          title="Refresh (r)"
          aria-busy={refreshing}
        >
          <RefreshIcon />
        </button>
        <button
          type="button"
          className="icon-btn"
          onClick={onToggleCompact}
          aria-pressed={settings.compact}
          aria-label="Compact rows"
          title={`Compact rows: ${settings.compact ? 'on' : 'off'} (c)`}
        >
          <CompactIcon />
        </button>
        <button
          type="button"
          className="icon-btn"
          onClick={onToggleMerged}
          aria-pressed={settings.showMerged}
          aria-label="Show merged pull requests"
          title={`Merged pull requests: ${settings.showMerged ? 'shown' : 'hidden'} (m)`}
        >
          <MergeIcon />
        </button>
        <button
          type="button"
          className="icon-btn"
          onClick={onCycleTheme}
          aria-label={`Theme: ${themeLabel}. Switch to the next theme`}
          title={`Theme: ${themeLabel} (t)`}
        >
          <ThemeIcon />
        </button>
      </div>
    </header>
  )
}
