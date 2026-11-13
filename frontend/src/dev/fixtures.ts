// Fixture snapshots for `npm run dev` in a plain browser. Pick one with
// ?fixture=<name>: sample (default), many, empty, norepo, warnings, error, loading.
import sample from './sample-snapshot.json'

export type FixtureName = 'sample' | 'many' | 'empty' | 'norepo' | 'warnings' | 'error' | 'loading'

// Plain JSON shape; converted to model classes by the fixture backend.
type Json = Record<string, any>

const TIME_KEYS = new Set(['time', 'mergedAt', 'lastCommitAt', 'updatedAt', 'generatedAt', 'fetchedAt'])

/** Shifts every timestamp so generatedAt becomes `now`, keeping relative times realistic. */
export function shiftTimes<T>(value: T, deltaMs: number): T {
  const walk = (v: any): any => {
    if (Array.isArray(v)) return v.map(walk)
    if (v && typeof v === 'object') {
      const out: Json = {}
      for (const [k, x] of Object.entries(v)) {
        if (TIME_KEYS.has(k) && typeof x === 'string' && !x.startsWith('0001-')) {
          out[k] = new Date(Date.parse(x) + deltaMs).toISOString()
        } else {
          out[k] = walk(x)
        }
      }
      return out
    }
    return v
  }
  return walk(value)
}

const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v))

function sha(n: number): string {
  return (n.toString(16).padStart(8, '0') + 'c0ffee'.repeat(6)).slice(0, 40)
}

/** ~120 branches over 40 anchors with stacks, long names, hidden ones and merged PRs. */
function many(): Json {
  const base = clone(sample) as Json
  const t0 = Date.parse(base.generatedAt)
  const iso = (minAgo: number) => new Date(t0 - minAgo * 60_000).toISOString()
  const statuses = ['review', 'approved', 'ci_failing', 'conflicts', 'draft', 'no_pr']
  const words = ['auth', 'export', 'importer', 'cache', 'search', 'billing', 'agents', 'telemetry', 'onboarding', 'perf']
  const anchors: Json[] = [base.main.anchors[0]]
  const branches: Json[] = []
  let pr = 500
  let behind = 0
  for (let a = 1; a <= 40; a++) {
    behind += 1 + ((a * 7) % 5)
    const names: string[] = []
    const merged: Json[] = []
    const n = a % 6 === 0 ? 0 : 1 + ((a * 3) % 4)
    for (let j = 0; j < n; j++) {
      const w = words[(a + j) % words.length]
      const long = (a + j) % 9 === 0
      const name = long
        ? `agent/${w}/very-long-branch-name-generated-by-an-overly-descriptive-coding-agent-${a}-${j}`
        : `${j % 2 ? 'fix' : 'feat'}/${w}-${a}-${j}`
      const status = statuses[(a + j) % statuses.length]
      const hasPR = status !== 'no_pr'
      pr++
      names.push(name)
      branches.push({
        name, local: (a + j) % 4 !== 0, remote: (a + j) % 5 !== 0, tipSha: sha(a * 100 + j), unpushed: (a + j) % 7 === 0 ? 2 : 0, unpulled: 0,
        parent: '', baseSha: sha(a), ahead: 1 + ((a + j) % 13), behind, lastCommitAt: iso(a * 90 + j * 7), status,
        pr: hasPR ? { number: pr, title: `${w} change ${a}.${j}`, url: `https://github.com/acme/my-app/pull/${pr}`, author: `agent-${j}`, isDraft: status === 'draft', baseRef: 'main', headRef: name, headSha: sha(a * 100 + j), reviewDecision: status === 'approved' ? 'APPROVED' : 'REVIEW_REQUIRED', checks: status === 'ci_failing' ? 'failing' : 'passing', failingChecks: status === 'ci_failing' ? ['test', 'e2e (chromium)'] : [], mergeable: status === 'conflicts' ? 'CONFLICTING' : 'MERGEABLE', updatedAt: iso(a * 80) } : null,
        worktree: (a + j) % 3 === 0 ? { path: `/home/dev/my-app-worktrees/${w}-${a}-${j}`, dirty: (a + j) % 2 === 0, isMain: false } : null,
        hidden: (a + j) % 11 === 0, hiddenReason: (a + j) % 11 === 0 ? 'stale (older than 14 days)' : '',
      })
      // Stack one or two branches on some of them.
      if ((a + j) % 5 === 1) {
        const child = `${name}-part2`
        pr++
        branches.push({
          ...clone(branches[branches.length - 1]), name: child, parent: name, ahead: 2, status: 'draft', hidden: false, hiddenReason: '',
          pr: { number: pr, title: `${w} follow-up`, url: `https://github.com/acme/my-app/pull/${pr}`, author: 'agent-9', isDraft: true, baseRef: name, headRef: child, headSha: sha(pr), reviewDecision: '', checks: 'pending', failingChecks: [], mergeable: 'MERGEABLE', updatedAt: iso(a * 70) },
        })
        if (a % 2 === 0) {
          branches.push({ ...clone(branches[branches.length - 1]), name: `${child}-part3`, parent: child, ahead: 1, status: 'no_pr', pr: null })
        }
      }
    }
    if (a % 4 === 2) {
      pr++
      merged.push({ number: pr, title: `Merged change ${a}`, url: `https://github.com/acme/my-app/pull/${pr}`, headRef: `feat/merged-${a}`, author: 'agent-1', mergedAt: iso(a * 60), mergeCommitSha: sha(a) })
    }
    if (names.length === 0 && merged.length === 0) continue
    anchors.push({ commit: { sha: sha(a), shortSha: sha(a).slice(0, 7), subject: `Commit ${a}`, time: iso(a * 60) }, behind, isHead: false, branchNames: names, merged })
  }
  base.main.anchors = anchors
  base.branches = branches
  return base
}

export function fixtureSnapshot(name: FixtureName): Json {
  switch (name) {
    case 'many':
      return many()
    case 'empty': {
      const s = clone(sample) as Json
      s.branches = []
      s.main.anchors = [s.main.anchors[0]]
      s.main.olderHidden = true
      return s
    }
    case 'norepo': {
      const s = clone(sample) as Json
      s.repo = { path: '', slug: '', remote: '', defaultBranch: '' }
      s.branches = []
      s.main.anchors = []
      return s
    }
    case 'warnings': {
      const s = clone(sample) as Json
      s.warnings = ['gh is not logged in — run `gh auth login` to see pull requests', 'git fetch failed: could not resolve host github.com']
      return s
    }
    default:
      return clone(sample) as Json
  }
}

export function fixtureNameFromURL(search: string): FixtureName | null {
  const p = new URLSearchParams(search)
  if (!p.has('fixture')) return null
  const v = p.get('fixture') || 'sample'
  const known: FixtureName[] = ['sample', 'many', 'empty', 'norepo', 'warnings', 'error', 'loading']
  return (known as string[]).includes(v) ? (v as FixtureName) : 'sample'
}
