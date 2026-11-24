import { describe, expect, it } from 'vitest'
import sample from './dev/sample-snapshot.json'
import { computeLayout, type BranchRow, type GapRow, type MergedRow, type Row } from './layout'
import type { Anchor, Branch, MergedPR, Snapshot } from './model'

const snap = sample as unknown as Snapshot
const ALL = { showMerged: true, showHidden: false }
const NO_MERGED = { showMerged: false, showHidden: false }

// ---- helpers ---------------------------------------------------------------

function commit(sha: string) {
  return { sha, shortSha: sha.slice(0, 7), subject: `commit ${sha}`, time: '2026-11-13T10:00:00Z' }
}

function anchor(sha: string, behind: number, extra: Partial<Anchor> = {}): Anchor {
  return { commit: commit(sha), behind, isHead: behind === 0, branchNames: [], merged: [], ...extra } as unknown as Anchor
}

function branch(name: string, behind: number, extra: Partial<Branch> = {}): Branch {
  return {
    name,
    local: true,
    remote: true,
    tipSha: `tip-${name}`,
    unpushed: 0,
    unpulled: 0,
    parent: '',
    baseSha: '',
    ahead: 1,
    behind,
    lastCommitAt: '2026-11-13T10:00:00Z',
    status: 'review',
    pr: null,
    worktree: null,
    hidden: false,
    hiddenReason: '',
    ...extra,
  } as unknown as Branch
}

function merged(n: number): MergedPR {
  return { number: n, title: `PR ${n}`, url: `https://x/${n}`, headRef: `h${n}`, author: 'a', mergedAt: '2026-11-13T10:00:00Z', mergeCommitSha: '' } as MergedPR
}

function snapshot(anchors: Anchor[], branches: Branch[], olderHidden = false, local: unknown = null): Snapshot {
  return {
    repo: { path: '/r', slug: 'o/r', remote: 'origin', defaultBranch: 'main' },
    main: { ref: 'origin/main', head: commit('head'), anchors, olderHidden, local },
    branches,
    generatedAt: '2026-11-13T10:00:00Z',
    fetchedAt: '2026-11-13T10:00:00Z',
    warnings: [],
  } as unknown as Snapshot
}

/** Compact textual picture of the rows, easier to assert on than objects. */
function shape(rows: Row[]): string[] {
  return rows.map((r) => {
    switch (r.kind) {
      case 'head':
        return r.local ? 'HEAD+local' : 'HEAD'
      case 'local':
        return 'LOCAL'
      case 'gap':
        return `gap ${r.count}`
      case 'tail':
        return 'tail'
      case 'merged':
        return `merged #${r.pr.number}`
      case 'branch':
        return `${'  '.repeat(r.depth)}${r.branch.name}`
    }
  })
}

const branchRow = (rows: Row[], name: string) =>
  rows.find((r): r is BranchRow => r.kind === 'branch' && r.branch.name === name)!

// ---- tests -----------------------------------------------------------------

describe('computeLayout with the sample snapshot', () => {
  it('orders rows newest → oldest with merged PRs and collapsed counts', () => {
    const { rows } = computeLayout(snap, ALL)
    expect(shape(rows)).toEqual([
      'HEAD',
      'gap 1',
      'LOCAL',
      'agent/fix-flaky-tests',
      'gap 2',
      'merged #421',
      'gap 3',
      'feat/auth-sso',
      '  feat/auth-sso-ui',
      // d0 (behind 12) only has a hidden branch → dropped; 15-9-1 = 5
      'gap 5',
      'merged #418',
      'gap 7',
      'feat/ifc-export',
      'chore/deps-bump',
      'gap 23',
      'fix/legacy-importer',
      'tail',
    ])
  })

  it('drops merged-only anchors and recomputes collapsed counts when merged is off', () => {
    const { rows } = computeLayout(snap, NO_MERGED)
    expect(shape(rows)).toEqual([
      'HEAD',
      'gap 1',
      'LOCAL',
      'agent/fix-flaky-tests',
      'gap 6', // 9 - 2 - 1
      'feat/auth-sso',
      '  feat/auth-sso-ui',
      'gap 13', // 23 - 9 - 1
      'feat/ifc-export',
      'chore/deps-bump',
      'gap 23', // 47 - 23 - 1
      'fix/legacy-importer',
      'tail',
    ])
    expect(rows.some((r) => r.kind === 'merged')).toBe(false)
  })

  it('shows hidden branches dimmed when showHidden is on and keeps their anchors', () => {
    const { rows, hiddenCount, shownCount } = computeLayout(snap, { showMerged: false, showHidden: true })
    expect(hiddenCount).toBe(2)
    expect(shownCount).toBe(8)
    expect(shape(rows)).toContain('dependabot/npm_and_yarn/vite-8.2.5')
    expect(branchRow(rows, 'dependabot/npm_and_yarn/vite-8.2.5').dimmed).toBe(true)
    expect(branchRow(rows, 'archive/old-spike').dimmed).toBe(true)
    expect(branchRow(rows, 'feat/auth-sso').dimmed).toBe(false)
    const gaps = rows.filter((r): r is GapRow => r.kind === 'gap').map((g) => g.count)
    expect(gaps).toEqual([1, 6, 2, 10, 23])
  })

  it('counts hidden branches even when they are not shown', () => {
    const { hiddenCount, shownCount } = computeLayout(snap, ALL)
    expect(hiddenCount).toBe(2)
    expect(shownCount).toBe(6)
  })

  it('marks anchor dots and connectors', () => {
    const { rows } = computeLayout(snap, ALL)
    expect(rows[0].mainDot).toBe('head')
    const sso = branchRow(rows, 'feat/auth-sso')
    expect(sso.mainDot).toBe('divergence')
    expect(sso.connector).toEqual({ kind: 'straight' })
    const ui = branchRow(rows, 'feat/auth-sso-ui')
    expect(ui.connector).toEqual({ kind: 'elbow', from: 0 })
    expect(ui.stackedOn).toEqual({ name: 'feat/auth-sso', prNumber: 424 })
    expect(ui.mainDot).toBeNull()
    expect(branchRow(rows, 'chore/deps-bump').connector).toEqual({ kind: 'elbow', from: 'junction' })
    const m = rows.find((r): r is MergedRow => r.kind === 'merged')!
    expect(m.mainDot).toBe('convergence')
    expect(m.connector).toEqual({ kind: 'merge' })
  })
})

describe('anchor dropping', () => {
  it('keeps HEAD even with nothing else', () => {
    const { rows } = computeLayout(snapshot([anchor('h', 0)], []), ALL)
    expect(shape(rows)).toEqual(['HEAD'])
    expect(rows[0].main).toEqual({ top: null, bottom: null })
  })

  it('synthesises HEAD when the snapshot has no anchors', () => {
    const { rows } = computeLayout(snapshot([], []), ALL)
    expect(shape(rows)).toEqual(['HEAD'])
  })

  it('drops anchors whose branches are all hidden and merged-only anchors when merged is off', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 3, { branchNames: ['x'] }), anchor('b', 5, { merged: [merged(7)] }), anchor('c', 9, { branchNames: ['y'] })],
      [branch('x', 3, { hidden: true }), branch('y', 9)],
    )
    expect(shape(computeLayout(s, NO_MERGED).rows)).toEqual(['HEAD', 'gap 8', 'y'])
    expect(shape(computeLayout(s, ALL).rows)).toEqual(['HEAD', 'gap 4', 'merged #7', 'gap 3', 'y'])
  })

  it('omits the collapsed pill when anchors are adjacent', () => {
    const s = snapshot([anchor('h', 0), anchor('a', 1, { branchNames: ['x'] })], [branch('x', 1)])
    expect(shape(computeLayout(s, ALL).rows)).toEqual(['HEAD', 'x'])
  })
})

describe('stacking', () => {
  it('nests children recursively under their parent in input order', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 4, { branchNames: ['base'] })],
      [branch('base', 4), branch('mid', 4, { parent: 'base' }), branch('leaf', 4, { parent: 'mid' }), branch('mid2', 4, { parent: 'base' })],
    )
    const { rows } = computeLayout(s, ALL)
    expect(shape(rows)).toEqual(['HEAD', 'gap 3', 'base', '  mid', '    leaf', '  mid2'])
    // Lane under base (depth 0) runs from base to mid2; lane under mid only reaches the adjacent leaf.
    const lanes = rows.map((r) => r.lanes)
    expect(lanes[2]).toEqual([{ col: 0, seg: 'start' }])
    expect(lanes[3]).toEqual([{ col: 1, seg: 'start' }, { col: 0, seg: 'full' }])
    expect(lanes[4]).toEqual([{ col: 0, seg: 'full' }])
    expect(lanes[5]).toEqual([])
    expect(branchRow(rows, 'leaf').connector).toEqual({ kind: 'elbow', from: 1 })
  })

  it('re-attaches a stacked branch to main when its parent is hidden', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 4, { branchNames: ['base'] }), anchor('b', 8, { branchNames: ['other'] })],
      [branch('base', 4, { hidden: true }), branch('child', 4, { parent: 'base' }), branch('other', 8)],
    )
    const { rows } = computeLayout(s, NO_MERGED)
    expect(shape(rows)).toEqual(['HEAD', 'gap 3', 'child', 'gap 3', 'other'])
    const child = branchRow(rows, 'child')
    expect(child.connector).toEqual({ kind: 'straight' })
    expect(child.stackedOn?.name).toBe('base')

    const shown = computeLayout(s, { showMerged: false, showHidden: true }).rows
    expect(shape(shown)).toEqual(['HEAD', 'gap 3', 'base', '  child', 'gap 3', 'other'])
  })

  it('falls back to the nearest anchor when no anchor matches behind', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 4, { branchNames: [] }), anchor('b', 20, { branchNames: ['other'] })],
      [branch('orphan', 6, { parent: 'gone' }), branch('other', 20)],
    )
    expect(shape(computeLayout(s, ALL).rows)).toEqual(['HEAD', 'gap 3', 'orphan', 'gap 15', 'other'])
  })

  it('survives parent cycles', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 2)],
      [branch('p', 2, { parent: 'q' }), branch('q', 2, { parent: 'p' })],
    )
    const names = shape(computeLayout(s, ALL).rows).map((x) => x.trim())
    expect(names).toEqual(['HEAD', 'gap 1', 'p', 'q'])
  })
})

describe('lanes and main line', () => {
  it('runs the junction lane from the anchor dot to the last sibling, through stacked rows', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 2, { branchNames: ['one', 'two', 'three'] })],
      [branch('one', 2), branch('one-kid', 2, { parent: 'one' }), branch('two', 2), branch('three', 2)],
    )
    const { rows } = computeLayout(s, ALL)
    expect(shape(rows)).toEqual(['HEAD', 'gap 1', 'one', '  one-kid', 'two', 'three'])
    expect(rows[2].lanes).toHaveLength(2)
    expect(rows[2].lanes).toContainEqual({ col: 0, seg: 'start' })
    expect(rows[2].lanes).toContainEqual({ col: 'junction', seg: 'start' })
    expect(rows[3].lanes).toContainEqual({ col: 'junction', seg: 'full' })
    expect(rows[4].lanes).toEqual([{ col: 'junction', seg: 'full' }])
    expect(rows[5].lanes).toEqual([])
    expect(branchRow(rows, 'three').connector).toEqual({ kind: 'elbow', from: 'junction' })
  })

  it('connects branches based at HEAD through the junction', () => {
    const s = snapshot([anchor('h', 0, { branchNames: ['fresh'] })], [branch('fresh', 0)])
    const { rows } = computeLayout(s, ALL)
    expect(shape(rows)).toEqual(['HEAD', 'fresh'])
    expect(rows[0].lanes).toEqual([{ col: 'junction', seg: 'start' }])
    expect(branchRow(rows, 'fresh').connector).toEqual({ kind: 'elbow', from: 'junction' })
  })

  it('ends main at the last anchor, or continues dashed into the tail', () => {
    const anchors = [anchor('h', 0), anchor('a', 3, { branchNames: ['x', 'y'] })]
    const branches = [branch('x', 3), branch('y', 3)]
    const closed = computeLayout(snapshot(anchors, branches, false), ALL).rows
    expect(closed.map((r) => r.main)).toEqual([
      { top: null, bottom: 'solid' },
      { top: 'solid', bottom: 'solid' }, // gap
      { top: 'solid', bottom: null }, // x carries the anchor dot
      { top: null, bottom: null },
    ])
    const open = computeLayout(snapshot(anchors, branches, true), ALL).rows
    expect(shape(open)).toEqual(['HEAD', 'gap 2', 'x', 'y', 'tail'])
    expect(open.slice(2).map((r) => r.main)).toEqual([
      { top: 'solid', bottom: 'dashed' },
      { top: 'dashed', bottom: 'dashed' },
      { top: 'dashed', bottom: null },
    ])
  })

  it('links merged PRs of the last anchor with a dashed link below the dot', () => {
    const s = snapshot([anchor('h', 0), anchor('a', 2, { branchNames: ['x'], merged: [merged(5)] })], [branch('x', 2)])
    const { rows } = computeLayout(s, ALL)
    expect(shape(rows)).toEqual(['HEAD', 'gap 1', 'x', 'merged #5'])
    expect(rows[2].mainDot).toBe('convergence')
    expect(rows[2].main).toEqual({ top: 'solid', bottom: 'link' })
    expect(rows[3].main).toEqual({ top: null, bottom: null })
  })
})

describe('scale and edge cases', () => {
  it('handles zero branches', () => {
    const s = snapshot([anchor('h', 0)], [], true)
    const l = computeLayout(s, ALL)
    expect(shape(l.rows)).toEqual(['HEAD', 'tail'])
    expect(l.shownCount).toBe(0)
  })

  it('handles 150 branches across 50 anchors', () => {
    const anchors = [anchor('h', 0)]
    const branches: Branch[] = []
    for (let i = 1; i <= 50; i++) {
      const names = [0, 1, 2].map((j) => `b-${i}-${j}`)
      anchors.push(anchor(`a${i}`, i * 3, { branchNames: names }))
      names.forEach((n) => branches.push(branch(n, i * 3)))
    }
    const { rows, shownCount } = computeLayout(snapshot(anchors, branches), ALL)
    expect(shownCount).toBe(150)
    expect(rows.filter((r) => r.kind === 'branch')).toHaveLength(150)
    expect(rows.filter((r) => r.kind === 'gap')).toHaveLength(50)
    expect(new Set(rows.map((r) => r.key)).size).toBe(rows.length)
  })
})

describe('local main', () => {
  const lm = (behind: number, ahead = 0) => ({ name: 'main', sha: 'l', shortSha: 'l', baseSha: '', ahead, behind, worktree: null })

  it('draws a local row carrying the anchor dot, siblings hang from the junction', () => {
    const { rows } = computeLayout(snap, ALL)
    const i = rows.findIndex((r) => r.kind === 'local')
    expect(rows[i].mainDot).toBe('local')
    const next = rows[i + 1] as BranchRow
    expect(next.branch.name).toBe('agent/fix-flaky-tests')
    expect(next.mainDot).toBeNull()
    expect(next.connector).toEqual({ kind: 'elbow', from: 'junction' })
    expect(rows[i].lanes).toEqual([{ col: 'junction', seg: 'start' }])
  })

  it('keeps an anchor that only holds the local main', () => {
    const s = snapshot([anchor('h', 0), anchor('m', 4, { isLocalMain: true })], [], false, lm(4))
    expect(shape(computeLayout(s, ALL).rows)).toEqual(['HEAD', 'gap 3', 'LOCAL'])
  })

  it('rides on the HEAD row when up to date', () => {
    const s = snapshot([anchor('h', 0, { isLocalMain: true })], [], false, lm(0, 2))
    const { rows } = computeLayout(s, ALL)
    expect(shape(rows)).toEqual(['HEAD+local'])
    expect(rows[0].mainDot).toBe('head')
  })

  it('ignores the flag when the snapshot has no local main', () => {
    const s = snapshot([anchor('h', 0), anchor('m', 4, { isLocalMain: true })], [])
    expect(shape(computeLayout(s, ALL).rows)).toEqual(['HEAD'])
  })
})

describe('only remote branches', () => {
  it('leaves local-only branches out and counts them', () => {
    const l = computeLayout(snap, { ...ALL, onlyRemote: true })
    const names = l.rows.filter((r): r is BranchRow => r.kind === 'branch').map((r) => r.branch.name)
    expect(names).not.toContain('chore/deps-bump')
    expect(names).toContain('feat/ifc-export')
    expect(names.every((n) => snap.branches.find((b) => b.name === n)!.remote)).toBe(true)
    // chore/deps-bump is local only and visible; archive/old-spike is local only but hidden.
    expect(l.localOnlyCount).toBe(1)
    expect(l.hiddenCount).toBe(1)
  })

  it('re-attaches a stacked branch to main when its parent is local only', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 3, { branchNames: ['p'] })],
      [branch('p', 3, { remote: false }), branch('c', 3, { parent: 'p' })],
    )
    const { rows } = computeLayout(s, { ...ALL, onlyRemote: true })
    expect(shape(rows)).toEqual(['HEAD', 'gap 2', 'c'])
  })
})

describe('only pull request branches', () => {
  const pr = { number: 1, title: 't', url: 'u' } as Branch['pr']

  it('leaves branches without a PR out and counts them', () => {
    const l = computeLayout(snap, { ...ALL, onlyPRs: true })
    const names = l.rows.filter((r): r is BranchRow => r.kind === 'branch').map((r) => r.branch.name)
    expect(names).not.toContain('chore/deps-bump')
    expect(names).toContain('feat/ifc-export')
    expect(names.every((n) => !!snap.branches.find((b) => b.name === n)!.pr)).toBe(true)
    // chore/deps-bump has no PR and is visible; archive/old-spike has none but is hidden.
    expect(l.noPRCount).toBe(1)
    expect(l.hiddenCount).toBe(1)
  })

  it('re-attaches a stacked branch to main when its parent has no PR', () => {
    const s = snapshot(
      [anchor('h', 0), anchor('a', 3, { branchNames: ['p'] })],
      [branch('p', 3), branch('c', 3, { parent: 'p', pr })],
    )
    const { rows } = computeLayout(s, { ...ALL, onlyPRs: true })
    expect(shape(rows)).toEqual(['HEAD', 'gap 2', 'c'])
  })

  it('does not count working copies as left out', () => {
    const s = snapshot(
      [anchor('h', 0, { branchNames: ['t3/rebase-x'] }), anchor('m', 5, { branchNames: ['feat/x'] })],
      [branch('t3/rebase-x', 0, { copyOf: 'feat/x', remote: false } as Partial<Branch>), branch('feat/x', 5, { pr })],
    )
    const l = computeLayout(s, { ...ALL, onlyPRs: true })
    expect(l.noPRCount).toBe(0)
    expect(l.rows.flatMap((r) => (r.kind === 'branch' ? [r.branch.name] : []))).toEqual(['feat/x'])
  })
})

describe('working copies', () => {
  it('folds a working copy into the card of the branch it copies', () => {
    const snap = snapshot(
      [anchor('h', 0, { branchNames: ['t3/rebase-x'] }), anchor('m', 5, { branchNames: ['feat/x'] })],
      [
        branch('t3/rebase-x', 0, { copyOf: 'feat/x', remote: false } as Partial<Branch>),
        branch('feat/x', 5, { copy: { name: 't3/rebase-x', ahead: 1, behind: 0, dirty: false } } as Partial<Branch>),
      ],
    )
    const names = computeLayout(snap, ALL).rows.flatMap((r) => (r.kind === 'branch' ? [r.branch.name] : []))
    expect(names).toEqual(['feat/x'])
  })

  it('keeps the copy when the branch it copies is not shown', () => {
    const snap = snapshot(
      [anchor('h', 0, { branchNames: ['t3/rebase-x'] }), anchor('m', 5, { branchNames: ['feat/x'] })],
      [branch('t3/rebase-x', 0, { copyOf: 'feat/x' } as Partial<Branch>), branch('feat/x', 5, { hidden: true })],
    )
    const names = computeLayout(snap, ALL).rows.flatMap((r) => (r.kind === 'branch' ? [r.branch.name] : []))
    expect(names).toEqual(['t3/rebase-x'])
  })
})
