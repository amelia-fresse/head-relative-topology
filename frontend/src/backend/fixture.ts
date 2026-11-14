// Fixture backend: serves src/dev fixtures, keeps settings and hide rules in
// localStorage and applies hide/unhide locally, so the full UI works in a
// plain browser (`npm run dev`, then open with ?fixture).
import { fixtureSnapshot, shiftTimes, type FixtureName } from '../dev/fixtures'
import { defaultSettings, type RepoConfig, type Settings, type Snapshot, type Tab } from '../model'
import type { Backend } from './types'

const SETTINGS_KEY = 'hrt.fixture.settings'
const CONFIG_KEY = 'hrt.fixture.repoConfig'
const HAND = 'hidden by hand'

function load<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : null
  } catch {
    return null
  }
}

function store(key: string, v: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(v))
  } catch {
    // Private mode or storage disabled: settings just won't persist.
  }
}

function globToRegExp(glob: string): RegExp {
  const esc = glob.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.')
  return new RegExp(`^${esc}$`)
}

const delay = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** Dev convenience: ?fixture&theme=dolly&compact&merged=0&hidden overrides stored settings. */
function urlOverrides(): Partial<Settings> {
  const p = new URLSearchParams(window.location.search)
  const flag = (k: string) => (p.has(k) ? p.get(k) !== '0' : undefined)
  const out: Partial<Settings> = {}
  if (p.get('theme')) out.theme = p.get('theme')!
  const compact = flag('compact')
  const merged = flag('merged')
  const hidden = flag('hidden')
  const remote = flag('remote')
  if (remote !== undefined) out.onlyRemote = remote
  if (compact !== undefined) out.compact = compact
  if (merged !== undefined) out.showMerged = merged
  if (hidden !== undefined) out.showHidden = hidden
  return out
}

/** A fixture snapshot moved to another repository path / slug, for extra tabs. */
function asRepo(name: FixtureName, slug: string): any {
  const s = fixtureSnapshot(name) as any
  s.repo = { ...s.repo, slug, path: `/home/dev/${slug.split('/')[1]}` }
  return s
}

function attentionOf(raw: any): string {
  const st = (raw.branches as any[]).filter((b) => !b.hidden).map((b) => b.status)
  return st.includes('conflicts') ? 'conflicts' : st.includes('ci_failing') ? 'ci_failing' : ''
}

export function createFixtureBackend(name: FixtureName = 'sample'): Backend {
  // Open tabs; each raw fixture is the source of truth for everything except hide rules.
  const repos: any[] =
    name === 'norepo' ? [] : [fixtureSnapshot(name) as any, asRepo('empty', 'acme/backend-api'), asRepo('many', 'acme/big-monorepo')]
  let active = repos.length > 0 ? 0 : -1
  const norepo = fixtureSnapshot('norepo') as any
  const current = () => (active >= 0 ? repos[active] : norepo)
  let raw = current()
  const listeners = new Set<(s: Snapshot) => void>()
  const tabListeners = new Set<(t: Tab[]) => void>()
  const tabs = (): Tab[] =>
    repos.map((r, i) => ({
      path: r.repo.path,
      name: (r.repo.slug || r.repo.path).split('/').pop(),
      slug: r.repo.slug,
      active: i === active,
      attention: attentionOf(r),
    })) as Tab[]
  const select = (i: number) => {
    active = i
    raw = current()
    const t = tabs()
    tabListeners.forEach((l) => l(t))
  }
  let fetchedAt = Date.now() - 2 * 60_000

  const initialConfig = (): RepoConfig => ({
    hiddenBranches: (raw.branches as any[]).filter((b) => b.hidden && b.hiddenReason === HAND).map((b) => b.name),
    hidePatterns: Array.from(
      new Set((raw.branches as any[]).filter((b) => b.hidden && b.hiddenReason.startsWith('pattern ')).map((b) => b.hiddenReason.slice(8))),
    ),
    hideNoCommits: false,
    hideRemoteOnly: false,
    staleDays: 0,
  })
  let config: RepoConfig = load<RepoConfig>(CONFIG_KEY) ?? initialConfig()

  const build = (): Snapshot => {
    const now = Date.now()
    const s = shiftTimes(raw, now - Date.parse(raw.generatedAt))
    s.fetchedAt = new Date(fetchedAt).toISOString()
    s.generatedAt = new Date(now).toISOString()
    const patterns = (config.hidePatterns ?? []).map((p) => [p, globToRegExp(p)] as const)
    for (const b of s.branches as any[]) {
      const pattern = patterns.find(([, re]) => re.test(b.name))
      if ((config.hiddenBranches ?? []).includes(b.name)) {
        b.hidden = true
        b.hiddenReason = HAND
      } else if (pattern) {
        b.hidden = true
        b.hiddenReason = `pattern ${pattern[0]}`
      } else if (b.hiddenReason === HAND || b.hiddenReason.startsWith('pattern ')) {
        b.hidden = false
        b.hiddenReason = ''
      }
    }
    return s as Snapshot
  }

  const emit = () => {
    const s = build()
    listeners.forEach((l) => l(s))
    return s
  }

  const ready = async (): Promise<void> => {
    if (name === 'loading') await new Promise(() => {})
    if (name === 'error') throw new Error('fixture: could not read repository (simulated error)')
  }

  return {
    kind: 'fixture',
    snapshot: async () => {
      await ready()
      return build()
    },
    refresh: async (fetch) => {
      await ready()
      await delay(400)
      if (fetch) fetchedAt = Date.now()
      return emit()
    },
    pickRepo: async () => {
      await delay(200)
      // Pretend the user picked another repository.
      const picked = asRepo('sample', 'acme/docs-site')
      let i = repos.findIndex((r) => r.repo.path === picked.repo.path)
      if (i < 0) i = repos.push(picked) - 1
      select(i)
      return emit()
    },
    tabs: async () => tabs(),
    switchRepo: async (path) => {
      const i = repos.findIndex((r) => r.repo.path === path)
      if (i < 0) throw new Error(`${path} is not open`)
      select(i)
      return build()
    },
    closeRepo: async (path) => {
      const i = repos.findIndex((r) => r.repo.path === path)
      if (i < 0) return build()
      const wasActive = i === active
      repos.splice(i, 1)
      if (repos.length === 0) select(-1)
      else if (wasActive) select(Math.min(i, repos.length - 1))
      else select(active > i ? active - 1 : active)
      return build()
    },
    getSettings: async () => ({ ...defaultSettings(), ...(load<Partial<Settings>>(SETTINGS_KEY) ?? {}), ...urlOverrides() }),
    saveSettings: async (s) => store(SETTINGS_KEY, s),
    hideBranch: async (branch) => {
      if (!(config.hiddenBranches ?? []).includes(branch)) config = { ...config, hiddenBranches: [...(config.hiddenBranches ?? []), branch] }
      store(CONFIG_KEY, config)
      return emit()
    },
    unhideBranch: async (branch) => {
      config = { ...config, hiddenBranches: (config.hiddenBranches ?? []).filter((n) => n !== branch) }
      store(CONFIG_KEY, config)
      return emit()
    },
    openURL: async (url) => {
      window.open(url, '_blank', 'noopener,noreferrer')
    },
    copyText: (text) => navigator.clipboard.writeText(text),
    onSnapshot: (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    onTabs: (cb) => {
      tabListeners.add(cb)
      return () => tabListeners.delete(cb)
    },
  }
}
