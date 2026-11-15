import type { Settings, Snapshot, Tab } from '../model'

/**
 * Backend is everything the UI needs from the Go service. Two implementations:
 * the Wails bindings (real app) and a fixture backend for `npm run dev`.
 */
export interface Backend {
  readonly kind: 'wails' | 'fixture'
  snapshot(): Promise<Snapshot>
  refresh(fetch: boolean): Promise<Snapshot>
  pickRepo(): Promise<Snapshot>
  /** Open repositories in tab order. */
  tabs(): Promise<Tab[]>
  switchRepo(path: string): Promise<Snapshot>
  /** Closes a tab; returns the snapshot of the tab that is active afterwards. */
  closeRepo(path: string): Promise<Snapshot>
  getSettings(): Promise<Settings>
  saveSettings(s: Settings): Promise<void>
  hideBranch(name: string): Promise<Snapshot>
  unhideBranch(name: string): Promise<Snapshot>
  openURL(url: string): Promise<void>
  /** Starts a T3 Code thread, in a new worktree, that rebases the branch onto main. */
  rebaseOnMain(branch: string): Promise<void>
  copyText(text: string): Promise<void>
  /** Subscribes to pushed snapshots; returns an unsubscribe function. */
  onSnapshot(cb: (s: Snapshot) => void): () => void
  onTabs(cb: (t: Tab[]) => void): () => void
}

/** A snapshot without a repository path means "no repository open". */
export function hasRepo(s: Snapshot | null | undefined): boolean {
  return !!s && !!s.repo && s.repo.path !== ''
}

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message || e.name
  if (typeof e === 'string') return e
  try {
    return JSON.stringify(e)
  } catch {
    return String(e)
  }
}
