import { memo, useRef, type FocusEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { relTime } from '../format'
import { COMPACT, NORMAL, contentX, dotX, type Geometry } from '../geometry'
import type { BranchRow, GapRow, HeadRow, LocalRow, MergedRow, Row } from '../layout'
import { AGENT_LABEL, AGENT_SHORT, agentStateOf, STATUS_LABEL, statusOf, locationOf, LOCATION_LABEL, type AgentThread, type Anchor, type Branch, type LocalMain, type MergedPR, type Snapshot } from '../model'
import { AgentIcon, MoreIcon, CloudIcon, LaptopIcon } from './icons'
import { Rails } from './Rails'

export type MapTarget =
  | { kind: 'branch'; branch: Branch; anchor: Anchor }
  | { kind: 'merged'; pr: MergedPR; anchor: Anchor }

export interface MapActions {
  /** Primary click on a branch / merged PR. */
  activate(t: MapTarget, el: HTMLElement): void
  /** Open the context menu at viewport coordinates; focus returns to `returnFocus` on close. */
  menu(t: MapTarget, at: { x: number; y: number }, returnFocus: HTMLElement | null): void
  tipShow(t: MapTarget, el: HTMLElement): void
  tipHide(): void
  /** Open a link (the agent thread badge). */
  openLink(url: string): void
}

interface MapProps {
  rows: Row[]
  snapshot: Snapshot
  compact: boolean
  now: number
  actions: MapActions
}

export function BranchMap({ rows, snapshot, compact, now, actions }: MapProps) {
  const g = compact ? COMPACT : NORMAL
  return (
    <ol className={`map density-${compact ? 'compact' : 'normal'}`} aria-label={`Branches relative to ${snapshot.main.ref || 'main'}`}>
      {rows.map((row) => {
        switch (row.kind) {
          case 'head':
            return <HeadRowView key={row.key} row={row} g={g} compact={compact} snapshot={snapshot} now={now} actions={actions} />
          case 'local':
            return <LocalRowView key={row.key} row={row} g={g} compact={compact} mainRef={snapshot.main.ref || 'main'} actions={actions} />
          case 'branch':
            return <BranchRowView key={row.key} row={row} g={g} compact={compact} actions={actions} />
          case 'merged':
            return <MergedRowView key={row.key} row={row} g={g} compact={compact} now={now} actions={actions} />
          case 'gap':
            return <GapRowView key={row.key} row={row} g={g} compact={compact} />
          case 'tail':
            return (
              <li key={row.key} className="row row-tail" style={{ height: g.tailRowH }} aria-hidden="true">
                <Rails g={g} main={row.main} lanes={row.lanes} connector={{ kind: 'none' }} mainDot={null} splitY="100%" dotY={0} depth={0} />
              </li>
            )
        }
      })}
    </ol>
  )
}

// ---------------------------------------------------------------------------

function HeadRowView({ row, g, compact, snapshot, now, actions }: { row: HeadRow; g: Geometry; compact: boolean; snapshot: Snapshot; now: number; actions: MapActions }) {
  const commit = row.anchor.commit
  const fetched = relTime(snapshot.fetchedAt, now)
  const sha = commit.shortSha || commit.sha.slice(0, 7)
  const metaText = fetched ? `${sha} · fetched ${fetched}` : sha
  return (
    <li className="row row-head" style={compact ? { height: 32 } : undefined}>
      <Rails g={g} main={row.main} lanes={row.lanes} connector={{ kind: 'none' }} mainDot={row.mainDot} splitY={g.dotY} dotY={g.dotY} depth={0} />
      {compact ? (
        <div className="head-line" style={{ marginLeft: g.headContentX }} title={commit.subject}>
          <span className="head-chip">{snapshot.main.ref || 'main'}</span>
          {row.local && <LocalTag local={row.local} mainRef={snapshot.main.ref || 'main'} compact={compact} actions={actions} />}
          <span className="c-meta">{fetched ? `${sha} · ${fetched}` : sha}</span>
        </div>
      ) : (
        <div className="card head-card" style={{ marginLeft: g.headContentX }} title={commit.subject}>
          <span className="title">
            <span className="name">{snapshot.main.ref || 'main'}</span>
            {row.local && <LocalTag local={row.local} mainRef={snapshot.main.ref || 'main'} compact={compact} actions={actions} />}
          </span>
          <span className="meta">{metaText}</span>
        </div>
      )}
    </li>
  )
}

// ---------------------------------------------------------------------------

function localTitle(l: LocalMain, mainRef: string): string {
  const parts: string[] = []
  if (l.behind > 0) parts.push(`${l.behind} commit${l.behind === 1 ? '' : 's'} behind ${mainRef}`)
  if (l.ahead > 0) parts.push(`${l.ahead} commit${l.ahead === 1 ? '' : 's'} not pushed`)
  if (parts.length === 0) parts.push(`up to date with ${mainRef}`)
  if (l.worktree?.dirty) parts.push('uncommitted changes')
  return `Local ${l.name || 'main'} (${l.shortSha}): ${parts.join(', ')}`
}

function LocalSync({ l }: { l: LocalMain }) {
  if (l.ahead <= 0 && l.behind <= 0) return null
  return (
    <span className="sync" aria-hidden="true">
      {l.ahead > 0 && <span className="sync-up">⇡{l.ahead}</span>}
      {l.behind > 0 && <span className="sync-down">⇣{l.behind}</span>}
    </span>
  )
}

/** Local main riding on the HEAD row (up to date, or only ahead). */
function LocalTag({ local, mainRef, compact, actions }: { local: LocalMain; mainRef: string; compact: boolean; actions: MapActions }) {
  const title = localTitle(local, mainRef)
  return (
    <span className="local-tag" title={title} aria-label={title}>
      <i className="local-glyph" aria-hidden="true" />
      <span className="local-name">{local.name || 'main'}</span>
      <LocalSync l={local} />
      {local.worktree?.dirty && <span className="c-dirty" aria-hidden="true" />}
      <ThreadBadge t={local.thread} count={local.activeThreads} compact={compact} actions={actions} />
    </span>
  )
}

/** Local main where it sits on the line: a diamond on main and a plain label, no card. */
function LocalRowView({ row, g, compact, mainRef, actions }: { row: LocalRow; g: Geometry; compact: boolean; mainRef: string; actions: MapActions }) {
  const h = g.localRowH
  const l = row.local
  const title = localTitle(l, mainRef)
  return (
    <li className="row row-local" style={{ height: h }} title={title} aria-label={title}>
      <Rails g={g} main={row.main} lanes={row.lanes} connector={{ kind: 'none' }} mainDot={row.mainDot} splitY={h / 2} dotY={h / 2} depth={0} />
      <span className="local-label" style={{ left: g.headContentX, height: h }}>
        <LaptopIcon size={12} />
        <span className="local-name">{l.name || 'main'}</span>
        <LocalSync l={l} />
        {l.worktree?.dirty && <span className="c-dirty" aria-hidden="true" />}
        <span className="local-sha">{l.shortSha}</span>
        <ThreadBadge t={l.thread} count={l.activeThreads} compact={compact} actions={actions} />
      </span>
    </li>
  )
}

// ---------------------------------------------------------------------------

function useRowHandlers(target: MapTarget, actions: MapActions) {
  const hitRef = useRef<HTMLButtonElement>(null)
  const openMenuFromKeyboard = (el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    actions.menu(target, { x: r.left + 16, y: r.top + Math.min(r.height, 28) }, el)
  }
  return {
    hitRef,
    li: {
      onMouseEnter: (e: MouseEvent<HTMLElement>) => actions.tipShow(target, e.currentTarget),
      onMouseLeave: () => actions.tipHide(),
      onContextMenu: (e: MouseEvent<HTMLElement>) => {
        e.preventDefault()
        actions.tipHide()
        actions.menu(target, { x: e.clientX, y: e.clientY }, hitRef.current)
      },
    },
    hit: {
      onClick: (e: MouseEvent<HTMLButtonElement>) => {
        actions.tipHide()
        actions.activate(target, e.currentTarget)
      },
      onFocus: (e: FocusEvent<HTMLButtonElement>) => {
        if (e.currentTarget.matches(':focus-visible')) actions.tipShow(target, e.currentTarget.closest('li') ?? e.currentTarget)
      },
      onBlur: () => actions.tipHide(),
      onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => {
        if ((e.shiftKey && e.key === 'F10') || e.key === 'ContextMenu') {
          e.preventDefault()
          actions.tipHide()
          openMenuFromKeyboard(e.currentTarget)
        }
      },
    },
    more: {
      onClick: (e: MouseEvent<HTMLButtonElement>) => {
        actions.tipHide()
        openMenuFromKeyboard(e.currentTarget)
      },
    },
  }
}

const Sep = () => (
  <span className="sep" aria-hidden="true">
    ·
  </span>
)

function Badge({ children, tone }: { children: ReactNode; tone?: string }) {
  return <span className={`badge${tone ? ` badge-${tone}` : ''}`}>{children}</span>
}

function branchLabel(b: Branch): string {
  const st = statusOf(b)
  const parts = [b.name, STATUS_LABEL[st]]
  if (b.pr) parts.push(`pull request #${b.pr.number}${b.pr.title ? `: ${b.pr.title}` : ''}`)
  parts.push(`${b.ahead} ahead`, b.behind === 0 ? 'up to date with main' : `${b.behind} behind`)
  parts.push(LOCATION_LABEL[locationOf(b)])
  if (b.unpushed > 0) parts.push(`${b.unpushed} unpushed`)
  if (b.unpulled > 0) parts.push(`${b.unpulled} not pulled`)
  if (b.worktree?.dirty) parts.push('uncommitted changes')
  if (b.thread) parts.push(`${AGENT_LABEL[agentStateOf(b.thread)]}: ${b.thread.title}`)
  if (b.hidden) parts.push('hidden')
  return parts.join(', ')
}

const BranchRowView = memo(function BranchRowView({
  row,
  g,
  compact,
  actions,
}: {
  row: BranchRow
  g: Geometry
  compact: boolean
  actions: MapActions
}) {
  const b = row.branch
  const st = statusOf(b)
  const target: MapTarget = { kind: 'branch', branch: b, anchor: row.anchor }
  const h = useRowHandlers(target, actions)
  const x = dotX(g, row.depth)
  const hitLeft = x - g.dotSize / 2 - 4
  const cx = contentX(g, row.depth)

  return (
    <li className={`row row-branch loc-${locationOf(b)}${row.dimmed ? ' is-dim' : ''}${b.pr ? ' has-pr' : ''}`} style={compact ? { height: 32 } : undefined} {...h.li}>
      <Rails
        g={g}
        main={row.main}
        lanes={row.lanes}
        connector={row.connector}
        mainDot={row.mainDot}
        splitY={g.dotY}
        dotY={g.dotY}
        depth={row.depth}
        tone={row.dimmed ? 'dim' : 'normal'}
      />
      {b.ahead > 0 && (
        <span className="ahead-pill" style={{ left: x - g.dotSize / 2 - 3, top: g.dotY }} aria-hidden="true">
          ↑{b.ahead}
        </span>
      )}
      <button
        ref={h.hitRef}
        type="button"
        className="hit"
        style={{ marginLeft: hitLeft, paddingLeft: cx - hitLeft }}
        aria-label={branchLabel(b)}
        {...h.hit}
      >
        <span
          className={`dot st-${st}`}
          style={{ left: x - g.dotSize / 2, top: g.dotY - g.dotSize / 2, width: g.dotSize, height: g.dotSize }}
        />
        {compact ? <CompactBranch b={b} actions={actions} /> : <BranchCard row={row} actions={actions} />}
      </button>
      <button type="button" className="more" aria-label={`Actions for ${b.name}`} aria-haspopup="menu" title="Actions (Shift+F10)" {...h.more}>
        <MoreIcon size={14} />
      </button>
    </li>
  )
})

/** Small icon before the name: laptop = only on this machine, cloud = only on the remote. */
function LocationMark({ b }: { b: Branch }) {
  const loc = locationOf(b)
  if (loc === 'both') return null
  return (
    <span className={`loc-mark loc-mark-${loc}`} title={LOCATION_LABEL[loc]}>
      {loc === 'local' ? <LaptopIcon size={13} /> : <CloudIcon size={13} />}
    </span>
  )
}

/** Out-of-sync counts between the local branch and its remote counterpart. */
function Sync({ b }: { b: Branch }) {
  if (b.unpushed <= 0 && b.unpulled <= 0) return null
  return (
    <span className="sync" aria-hidden="true">
      {b.unpushed > 0 && (
        <span className="sync-up" title={`${b.unpushed} commit${b.unpushed === 1 ? '' : 's'} not pushed`}>
          ⇡{b.unpushed}
        </span>
      )}
      {b.unpulled > 0 && (
        <span className="sync-down" title={`${b.unpulled} commit${b.unpulled === 1 ? '' : 's'} not pulled`}>
          ⇣{b.unpulled}
        </span>
      )}
    </span>
  )
}

/**
 * T3 Code thread working on the branch. It sits inside the row's button, so it
 * is mouse-only (aria-hidden); keyboard users get "Open agent thread" in the
 * row's menu and the state in the row's label.
 */
function AgentBadge({ b, compact, actions }: { b: Branch; compact: boolean; actions: MapActions }) {
  return <ThreadBadge t={b.thread} compact={compact} actions={actions} />
}

/** `count` > 1 says how many threads are active, `t` being the most urgent. */
function ThreadBadge({ t, count = 1, compact, actions }: { t: AgentThread | null | undefined; count?: number; compact: boolean; actions: MapActions }) {
  if (!t) return null
  const st = agentStateOf(t)
  const open = (e: MouseEvent<HTMLElement>) => {
    e.stopPropagation()
    actions.tipHide()
    if (t.link) actions.openLink(t.link)
  }
  const more = count > 1 ? ` (+${count - 1} more agent${count === 2 ? '' : 's'})` : ''
  return (
    <span className={`agent agent-${st}`} aria-hidden="true" title={`${AGENT_LABEL[st]}: ${t.title}${more} (click to open)`} onClick={open}>
      <AgentIcon size={compact ? 12 : 12} />
      {!compact && <span className="agent-word">{AGENT_SHORT[st]}</span>}
      {count > 1 && <span className="agent-count">{count}</span>}
    </span>
  )
}

function CompactBranch({ b, actions }: { b: Branch; actions: MapActions }) {
  return (
    <span className="c-line">
      <LocationMark b={b} />
      {b.pr?.title && (
        <span className="c-title" title={b.pr.title}>
          {b.pr.title}
        </span>
      )}
      <span className={b.pr?.title ? 'name c-branch' : 'name'} title={b.name}>
        {b.name}
      </span>
      {b.worktree?.dirty && <span className="c-dirty" aria-hidden="true" />}
      <AgentBadge b={b} compact actions={actions} />
      <span className="c-meta" aria-hidden="true">
        <Sync b={b} />
        {b.pr && <span>#{b.pr.number}</span>}
        <span>↑{b.ahead}</span>
        <span>↓{b.behind}</span>
      </span>
    </span>
  )
}

function BranchCard({ row, actions }: { row: BranchRow; actions: MapActions }) {
  const b = row.branch
  const st = statusOf(b)
  return (
    <span className="card">
      <span className="title">
        <LocationMark b={b} />
        {b.pr?.title ? (
          <span className="pr-title" title={b.pr.title}>
            {b.pr.title}
          </span>
        ) : (
          <span className="name" title={b.name}>
            {b.name}
          </span>
        )}
        <Sync b={b} />
        <AgentBadge b={b} compact={false} actions={actions} />
      </span>
      {b.pr?.title && (
        <span className="name branch-sub" title={b.name}>
          {b.name}
        </span>
      )}
      <span className="meta">
        {b.pr && (
          <>
            <span className="pr-num">#{b.pr.number}</span>
            <Sep />
          </>
        )}
        <span className={`st-word st-${st}`}>{STATUS_LABEL[st]}</span>
        <Sep />
        <span>{b.behind === 0 ? 'up to date' : `${b.behind} behind`}</span>
        {b.worktree?.dirty && (
          <Badge tone="dirty">
            <span className="badge-dot" aria-hidden="true" />
            uncommitted
          </Badge>
        )}
        {row.stackedOn && (
          <Badge tone="stack">stacked on {row.stackedOn.prNumber ? `#${row.stackedOn.prNumber}` : row.stackedOn.name}</Badge>
        )}
        {row.dimmed && <Badge tone="hidden">hidden</Badge>}
      </span>
    </span>
  )
}

// ---------------------------------------------------------------------------

const MergedRowView = memo(function MergedRowView({
  row,
  g,
  compact,
  now,
  actions,
}: {
  row: MergedRow
  g: Geometry
  compact: boolean
  now: number
  actions: MapActions
}) {
  const pr = row.pr
  const target: MapTarget = { kind: 'merged', pr, anchor: row.anchor }
  const h = useRowHandlers(target, actions)
  const x = dotX(g, 0)
  const hitLeft = x - g.mergedDotSize / 2 - 4
  const when = relTime(pr.mergedAt, now)
  const lineH = compact ? 18 : 20
  const splitY = row.mainDot ? g.mergeAnchorY : g.mergeDotY - g.mergeR
  return (
    <li className="row row-merged" style={{ height: g.mergedRowH }} {...h.li}>
      <Rails g={g} main={row.main} lanes={row.lanes} connector={row.connector} mainDot={row.mainDot} splitY={splitY} dotY={g.mergeDotY} depth={0} />
      <button
        ref={h.hitRef}
        type="button"
        className="hit merged-hit"
        style={{ left: hitLeft, top: g.mergeDotY - lineH / 2, height: lineH, paddingLeft: x + g.mergedDotSize / 2 + g.contentGap - hitLeft }}
        aria-label={`Pull request #${pr.number} merged${when ? ` ${when}` : ''}: ${pr.title}`}
        {...h.hit}
      >
        <span
          className="merged-dot"
          style={{ left: x - g.mergedDotSize / 2 - hitLeft, top: lineH / 2 - g.mergedDotSize / 2, width: g.mergedDotSize, height: g.mergedDotSize }}
        />
        <span className="merged-label">
          <span className="merged-num">#{pr.number}</span> merged{when ? ` · ${when}` : ''}
        </span>
        {!compact && pr.title && <span className="merged-title">{pr.title}</span>}
      </button>
    </li>
  )
})

// ---------------------------------------------------------------------------

function GapRowView({ row, g, compact }: { row: GapRow; g: Geometry; compact: boolean }) {
  const label = `${row.count} ${row.count === 1 ? 'commit' : 'commits'}`
  return (
    <li className="row row-gap" style={{ height: g.gapRowH }}>
      <Rails g={g} main={row.main} lanes={row.lanes} connector={{ kind: 'none' }} mainDot={null} splitY="50%" dotY={0} depth={0} />
      <span className="count-pill" style={{ left: g.mainX - (compact ? 8 : 9) }} title={`${label} on main not shown`}>
        {compact ? row.count : label}
        <span className="sr-only"> on main not shown</span>
      </span>
    </li>
  )
}
