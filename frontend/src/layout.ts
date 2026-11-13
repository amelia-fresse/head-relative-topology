// layout turns a Snapshot into the ordered list of rows the map draws from top
// to bottom. It is pure (no DOM, no time) so it can be unit-tested.
//
// Vocabulary
//   group     – the rows belonging to one visible main anchor. The first row
//               of a group carries the anchor's dot on the main line.
//   lane      – a vertical connector that runs through rows: the shared
//               junction just right of main (siblings of one anchor) or the
//               line hanging under a branch dot (its stacked children).
//   connector – how a row's own dot is linked: straight from main, elbow from
//               a lane, or (merged PRs) a dashed elbow joining main from below.
//   main      – per row, the style of the main line above and below the row's
//               split point (the anchor dot, or the row middle).
import type { Anchor, Branch, LayoutSettings, MergedPR, Snapshot } from './model'

/** 'junction' is the shared lane right of main; a number is the depth of the branch dot the lane hangs from. */
export type LaneCol = 'junction' | number

export interface Lane {
  col: LaneCol
  /** full: passes through the whole row. start: from the row's dot down to the bottom. */
  seg: 'full' | 'start'
}

/**
 * solid  – the main line.
 * dashed – main continues with older history not shown (olderHidden).
 * link   – thin dashed merged-colour line carrying a merged PR up to an anchor
 *          that is the last thing on the map (main itself ends there).
 */
export type MainSeg = 'solid' | 'dashed' | 'link'

export interface MainSegs {
  top: MainSeg | null
  bottom: MainSeg | null
}

export type MainDot = 'head' | 'divergence' | 'convergence'

export type Connector =
  | { kind: 'none' }
  | { kind: 'straight' }
  | { kind: 'elbow'; from: LaneCol }
  | { kind: 'merge' }

interface RowBase {
  key: string
  main: MainSegs
  lanes: Lane[]
  /** Set on the first row of a group: the anchor's dot on main is drawn in this row. */
  mainDot: MainDot | null
}

export interface HeadRow extends RowBase {
  kind: 'head'
  anchor: Anchor
}

export interface BranchRow extends RowBase {
  kind: 'branch'
  branch: Branch
  anchor: Anchor
  /** 0 for main-based branches, +1 per stacking level. */
  depth: number
  connector: Connector
  /** The branch is hidden by a rule but shown because settings.showHidden is on. */
  dimmed: boolean
  /** Set for stacked branches (even when re-attached to main because the parent is hidden). */
  stackedOn: { name: string; prNumber: number | null } | null
}

export interface MergedRow extends RowBase {
  kind: 'merged'
  pr: MergedPR
  anchor: Anchor
  connector: Connector
}

export interface GapRow extends RowBase {
  kind: 'gap'
  /** Commits on main collapsed between the two neighbouring anchors (always > 0). */
  count: number
}

export interface TailRow extends RowBase {
  kind: 'tail'
}

export type Row = HeadRow | BranchRow | MergedRow | GapRow | TailRow

export interface Layout {
  rows: Row[]
  /** Branches hidden by rules (regardless of showHidden). */
  hiddenCount: number
  /** Branches drawn on the map. */
  shownCount: number
}

// ---------------------------------------------------------------------------

function headAnchorFallback(s: Snapshot): Anchor {
  return {
    commit: s.main.head,
    behind: 0,
    isHead: true,
    branchNames: [],
    merged: [],
  } as Anchor
}

/** Anchors newest → oldest with exactly one HEAD first. */
function normaliseAnchors(s: Snapshot): Anchor[] {
  const anchors = [...(s.main?.anchors ?? [])]
  const headIdx = anchors.findIndex((a) => a.isHead)
  if (headIdx > 0) {
    const [h] = anchors.splice(headIdx, 1)
    anchors.unshift(h)
  } else if (headIdx < 0) {
    if (anchors.length > 0 && anchors[0].behind === 0) {
      anchors[0] = { ...anchors[0], isHead: true } as Anchor
    } else {
      anchors.unshift(headAnchorFallback(s))
    }
  }
  return anchors
}

/** Index of the anchor a root branch hangs from: listed in branchNames, else same behind, else nearest. */
function anchorIndexFor(b: Branch, anchors: Anchor[]): number {
  if (b.parent === '') {
    const listed = anchors.findIndex((a) => (a.branchNames ?? []).includes(b.name))
    if (listed >= 0) return listed
  }
  const exact = anchors.findIndex((a) => a.behind === b.behind)
  if (exact >= 0) return exact
  let best = 0
  let bestDist = Infinity
  anchors.forEach((a, i) => {
    const d = Math.abs(a.behind - b.behind)
    if (d < bestDist) {
      best = i
      bestDist = d
    }
  })
  return best
}

export function computeLayout(snapshot: Snapshot, settings: LayoutSettings): Layout {
  const anchors = normaliseAnchors(snapshot)
  const branches = snapshot.branches ?? []
  const visible = branches.filter((b) => !b.hidden || settings.showHidden)
  const byName = new Map(visible.map((b) => [b.name, b]))
  const allByName = new Map(branches.map((b) => [b.name, b]))

  // A branch is stacked when its parent is visible and the chain does not loop.
  const isStacked = (b: Branch): boolean => {
    if (!b.parent || !byName.has(b.parent)) return false
    const seen = new Set([b.name])
    let cur = byName.get(b.parent)
    while (cur) {
      if (seen.has(cur.name)) return false
      seen.add(cur.name)
      cur = cur.parent ? byName.get(cur.parent) : undefined
    }
    return true
  }

  const children = new Map<string, Branch[]>()
  const roots = new Map<number, Branch[]>()
  for (const b of visible) {
    if (isStacked(b)) {
      const list = children.get(b.parent) ?? []
      list.push(b)
      children.set(b.parent, list)
    } else {
      const idx = anchorIndexFor(b, anchors)
      const list = roots.get(idx) ?? []
      list.push(b)
      roots.set(idx, list)
    }
  }
  // Within an anchor, keep the backend's branchNames order first, then the rest in input order.
  for (const [idx, list] of roots) {
    const order = anchors[idx].branchNames ?? []
    const rank = (b: Branch) => {
      const i = order.indexOf(b.name)
      return i < 0 ? order.length : i
    }
    list.sort((a, b) => rank(a) - rank(b))
  }

  const keep = (a: Anchor, i: number) =>
    a.isHead || (roots.get(i)?.length ?? 0) > 0 || (settings.showMerged && (a.merged ?? []).length > 0)

  // --- Pass 1: emit rows with connectors; remember lane spans. -------------
  type Draft = Omit<HeadRow, 'main' | 'lanes'> | Omit<BranchRow, 'main' | 'lanes'> | Omit<MergedRow, 'main' | 'lanes'> | Omit<GapRow, 'main' | 'lanes'> | Omit<TailRow, 'main' | 'lanes'>
  const drafts: Draft[] = []
  const spans: { col: LaneCol; start: number; end: number }[] = []
  const dotRows: number[] = []
  let lastGroupStart = 0
  let prev: Anchor | null = null
  let shownCount = 0

  const emitBranch = (b: Branch, anchor: Anchor, depth: number, connector: Connector, mainDot: MainDot | null) => {
    const row = drafts.length
    const parent = b.parent ? allByName.get(b.parent) : undefined
    drafts.push({
      kind: 'branch',
      key: `b:${b.name}`,
      branch: b,
      anchor,
      depth,
      connector,
      dimmed: b.hidden,
      mainDot,
      stackedOn: b.parent ? { name: b.parent, prNumber: parent?.pr?.number ?? null } : null,
    })
    shownCount++
    const kids = children.get(b.name) ?? []
    let lastKid = -1
    for (const k of kids) {
      lastKid = drafts.length
      emitBranch(k, anchor, depth + 1, { kind: 'elbow', from: depth }, null)
    }
    if (lastKid >= 0) spans.push({ col: depth, start: row, end: lastKid })
  }

  anchors.forEach((anchor, i) => {
    if (!keep(anchor, i)) return
    if (prev) {
      const count = anchor.behind - prev.behind - 1
      if (count > 0) drafts.push({ kind: 'gap', key: `g:${anchor.commit.sha}`, count, mainDot: null })
    }
    prev = anchor

    const groupStart = drafts.length
    lastGroupStart = groupStart
    dotRows.push(groupStart)
    const dot: MainDot = anchor.isHead
      ? 'head'
      : settings.showMerged && (anchor.merged ?? []).length > 0
        ? 'convergence'
        : 'divergence'
    let junctionEnd = -1

    if (anchor.isHead) drafts.push({ kind: 'head', key: 'head', anchor, mainDot: dot })

    const groupRoots = roots.get(i) ?? []
    groupRoots.forEach((b) => {
      const first = drafts.length === groupStart
      if (!first) junctionEnd = drafts.length
      emitBranch(
        b,
        anchor,
        0,
        first ? { kind: 'straight' } : { kind: 'elbow', from: 'junction' },
        first ? dot : null,
      )
    })
    if (junctionEnd >= 0) spans.push({ col: 'junction', start: groupStart, end: junctionEnd })

    if (settings.showMerged) {
      for (const pr of anchor.merged ?? []) {
        const first = drafts.length === groupStart
        drafts.push({
          kind: 'merged',
          key: `m:${pr.number}:${anchor.commit.sha}`,
          pr,
          anchor,
          connector: { kind: 'merge' },
          mainDot: first ? dot : null,
        })
      }
    }
  })

  const olderHidden = !!snapshot.main?.olderHidden
  if (olderHidden) drafts.push({ kind: 'tail', key: 'tail', mainDot: null })

  // --- Pass 2: lanes and main segments. ------------------------------------
  const lanes: Lane[][] = drafts.map(() => [])
  for (const s of spans) {
    lanes[s.start].push({ col: s.col, seg: 'start' })
    for (let r = s.start + 1; r < s.end; r++) lanes[r].push({ col: s.col, seg: 'full' })
  }

  const lastDot = dotRows[dotRows.length - 1] ?? 0
  let lastAttach = lastDot
  for (let r = lastGroupStart; r < drafts.length; r++) {
    if (drafts[r].kind === 'merged') lastAttach = r
  }

  const rows: Row[] = drafts.map((d, r) => {
    let main: MainSegs
    if (r < lastDot) {
      main = { top: r === 0 ? null : 'solid', bottom: 'solid' }
    } else if (r === lastDot) {
      main = {
        top: r === 0 ? null : 'solid',
        bottom: olderHidden ? 'dashed' : lastAttach > r ? 'link' : null,
      }
    } else if (olderHidden) {
      main = { top: 'dashed', bottom: d.kind === 'tail' ? null : 'dashed' }
    } else if (r <= lastAttach) {
      // The merged elbow draws its own vertical from the row top.
      main = { top: d.kind === 'merged' ? null : 'link', bottom: r < lastAttach ? 'link' : null }
    } else {
      main = { top: null, bottom: null }
    }
    return { ...d, main, lanes: lanes[r] } as Row
  })

  return { rows, hiddenCount: branches.filter((b) => b.hidden).length, shownCount }
}
