import { relTime } from '../format'
import type { Settings, Snapshot } from '../model'
import { CloudIcon, CompactIcon, MergeIcon, PullRequestIcon, RefreshIcon, ThemeIcon } from './icons'

interface Props {
  snapshot: Snapshot | null
  settings: Settings
  now: number
  refreshing: boolean
  onRefresh(): void
  onToggleCompact(): void
  onToggleMerged(): void
  onToggleOnlyRemote(): void
  onToggleOnlyPRs(): void
  onCycleTheme(): void
}

const THEME_LABEL: Record<string, string> = { default: 'Default', babydoll: 'Babydoll', dolly: 'Dolly' }

export function Header({ snapshot, settings, now, refreshing, onRefresh, onToggleCompact, onToggleMerged, onToggleOnlyRemote, onToggleOnlyPRs, onCycleTheme }: Props) {
  const repo = snapshot?.repo
  const fetched = snapshot ? relTime(snapshot.fetchedAt, now) : null
  const themeLabel = THEME_LABEL[settings.theme] ?? settings.theme
  return (
    <header className="header">
      <div className="header-text">
        <div className="header-top">
          <span className="app-name" title="HEAD-Relative Topology">
            hrt
          </span>
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
          onClick={onToggleOnlyRemote}
          aria-pressed={settings.onlyRemote}
          aria-label="Only remote branches"
          title={`Only remote branches: ${settings.onlyRemote ? 'on' : 'off'} (l)`}
        >
          <CloudIcon />
        </button>
        <button
          type="button"
          className="icon-btn"
          onClick={onToggleOnlyPRs}
          aria-pressed={settings.onlyPRs}
          aria-label="Only branches with a pull request"
          title={`Only branches with a pull request: ${settings.onlyPRs ? 'on' : 'off'} (p)`}
        >
          <PullRequestIcon />
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
