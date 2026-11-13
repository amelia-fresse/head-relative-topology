import type { Settings, Snapshot } from '../model'

/**
 * Backend is everything the UI needs from the Go service. Two implementations:
 * the Wails bindings (real app) and a fixture backend for `npm run dev`.
 */
export interface Backend {
  readonly kind: 'wails' | 'fixture'
  snapshot(): Promise<Snapshot>
  refresh(fetch: boolean): Promise<Snapshot>
  pickRepo(): Promise<Snapshot>
  getSettings(): Promise<Settings>
  saveSettings(s: Settings): Promise<void>
  hideBranch(name: string): Promise<Snapshot>
  unhideBranch(name: string): Promise<Snapshot>
  openURL(url: string): Promise<void>
  copyText(text: string): Promise<void>
  /** Subscribes to pushed snapshots; returns an unsubscribe function. */
  onSnapshot(cb: (s: Snapshot) => void): () => void
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
