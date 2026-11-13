import { fixtureNameFromURL } from '../dev/fixtures'
import type { Snapshot } from '../model'
import { createFixtureBackend } from './fixture'
import { errorMessage, type Backend } from './types'
import { createWailsBackend } from './wails'

export type { Backend } from './types'
export { errorMessage, hasRepo } from './types'
export { createWailsBackend } from './wails'

export interface Connection {
  backend: Backend
  snapshot: Snapshot
  /** Set when we fell back to fixture data; shown as a notice. */
  fallbackReason: string | null
}

const PROBE_TIMEOUT_MS = 4000

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error(`no answer from the backend after ${ms / 1000}s`)), ms)
    p.then(
      (v) => {
        clearTimeout(t)
        resolve(v)
      },
      (e) => {
        clearTimeout(t)
        reject(e)
      },
    )
  })
}

/** RuntimeError means the Go method ran and returned an error; anything else means no Wails runtime. */
function isBackendError(e: unknown): boolean {
  return e instanceof Error && e.name === 'RuntimeError'
}

/**
 * connect picks the backend and loads the first snapshot.
 *  - `?fixture[=name]` in the URL → fixture backend.
 *  - otherwise Wails; if Snapshot() fails because the runtime is unreachable,
 *    or fails at all in a dev build, fall back to the sample fixture.
 * A real backend error in a production build is rethrown (shown with Retry).
 */
export async function connect(): Promise<Connection> {
  const fixture = fixtureNameFromURL(window.location.search)
  if (fixture) {
    const backend = createFixtureBackend(fixture)
    return { backend, snapshot: await backend.snapshot(), fallbackReason: null }
  }
  const wails = createWailsBackend()
  try {
    // In dev the bindings may point at nothing (plain browser): don't hang. In a
    // build, the first snapshot can legitimately take a while (fetch, gh).
    const first = import.meta.env.DEV ? withTimeout(wails.snapshot(), PROBE_TIMEOUT_MS) : wails.snapshot()
    return { backend: wails, snapshot: await first, fallbackReason: null }
  } catch (e) {
    if (import.meta.env.DEV || !isBackendError(e)) {
      const backend = createFixtureBackend('sample')
      return {
        backend,
        snapshot: await backend.snapshot(),
        fallbackReason: `Showing sample data: the backend is unavailable (${errorMessage(e)}).`,
      }
    }
    throw e
  }
}
