// Re-exports of the generated data contract (internal/model/model.go) plus a
// few frontend-side helpers. Only types are imported from the bindings here so
// pure modules (layout, format) stay free of runtime side effects.
import type {
  Anchor,
  Branch,
  Commit,
  LocalMain,
  MergedPR,
  PullRequest,
  RepoConfig,
  Settings,
  Snapshot,
  Worktree,
} from '../bindings/github.com/amelia-fresse/head-relative-topology/internal/model/models'

export type { Anchor, Branch, Commit, LocalMain, MergedPR, PullRequest, RepoConfig, Settings, Snapshot, Worktree }

export type StatusKey = 'no_pr' | 'draft' | 'conflicts' | 'ci_failing' | 'approved' | 'review'

const STATUS_KEYS: readonly StatusKey[] = ['no_pr', 'draft', 'conflicts', 'ci_failing', 'approved', 'review']

/** statusOf narrows the generated enum to a plain string union (unknown → no_pr). */
export function statusOf(b: Pick<Branch, 'status'>): StatusKey {
  const s = b.status as unknown as string
  return (STATUS_KEYS as readonly string[]).includes(s) ? (s as StatusKey) : 'no_pr'
}

export const STATUS_LABEL: Record<StatusKey, string> = {
  no_pr: 'no PR',
  draft: 'draft',
  conflicts: 'conflicts',
  ci_failing: 'CI failing',
  approved: 'approved',
  review: 'in review',
}

export type ThemeName = 'default' | 'babydoll' | 'dolly'
export const THEMES: readonly ThemeName[] = ['default', 'babydoll', 'dolly']

export function themeOf(s: Pick<Settings, 'theme'>): ThemeName {
  return (THEMES as readonly string[]).includes(s.theme) ? (s.theme as ThemeName) : 'default'
}

export function nextTheme(t: string): ThemeName {
  const i = (THEMES as readonly string[]).indexOf(t)
  return THEMES[(i + 1) % THEMES.length]
}

/** Mirrors model.DefaultSettings() in Go. */
export function defaultSettings(): Settings {
  return {
    theme: 'default',
    compact: false,
    showMerged: true,
    showHidden: false,
    onlyRemote: false,
    alwaysOnTop: false,
    fetchIntervalSec: 120,
    prPollIntervalSec: 60,
    mergedWindowHours: 48,
    lastRepo: '',
  }
}

/** The subset of settings the layout depends on. */
export type LayoutSettings = Pick<Settings, 'showMerged' | 'showHidden'> & Partial<Pick<Settings, 'onlyRemote'>>

export type Location = 'both' | 'local' | 'remote'

/** Where a branch lives: on both sides, only on this machine, or only on the remote. */
export function locationOf(b: Pick<Branch, 'local' | 'remote'>): Location {
  if (b.local && !b.remote) return 'local'
  if (!b.local && b.remote) return 'remote'
  return 'both'
}

export const LOCATION_LABEL: Record<Location, string> = {
  both: 'local and pushed',
  local: 'local only, not pushed',
  remote: 'remote only, not checked out',
}
