import { useLayoutEffect, useRef, useState } from 'react'
import { relTime } from '../format'
import { STATUS_LABEL, statusOf, type Branch } from '../model'
import type { MapTarget } from './BranchMap'

export interface TipState {
  target: MapTarget
  rect: DOMRect
}

const MARGIN = 8

/** Floating details for the hovered / focused row. Positioned below the row, or above when there is no room. */
export function Tooltip({ tip, now, mainRef }: { tip: TipState; now: number; mainRef: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const { rect } = tip
    const w = el.offsetWidth
    const h = el.offsetHeight
    const vw = window.innerWidth
    const vh = window.innerHeight
    const left = Math.max(MARGIN, Math.min(rect.left + 40, vw - w - MARGIN))
    const below = rect.bottom + 4
    const top = below + h <= vh - MARGIN ? below : Math.max(MARGIN, rect.top - h - 4)
    setPos({ left, top })
  }, [tip])

  return (
    <div
      ref={ref}
      role="tooltip"
      className="tooltip"
      style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0, visibility: 'hidden' }}
    >
      {tip.target.kind === 'branch' ? (
        <BranchTip b={tip.target.branch} now={now} mainRef={mainRef} anchorSha={tip.target.anchor.commit.shortSha} anchorSubject={tip.target.anchor.commit.subject} />
      ) : (
        <>
          <div className="tip-title">
            #{tip.target.pr.number} {tip.target.pr.title}
          </div>
          <div className="tip-line">
            Merged {relTime(tip.target.pr.mergedAt, now) ?? ''}
            {tip.target.pr.author ? ` · by ${tip.target.pr.author}` : ''}
          </div>
          {tip.target.pr.headRef && <div className="tip-line mono">{tip.target.pr.headRef}</div>}
          <div className="tip-line muted">
            Landed as {tip.target.anchor.commit.shortSha} {tip.target.anchor.commit.subject}
          </div>
        </>
      )}
    </div>
  )
}

function statusDetail(b: Branch): string {
  const st = statusOf(b)
  switch (st) {
    case 'ci_failing':
      return 'CI failing'
    case 'conflicts':
      return 'Merge conflicts with the base branch'
    case 'approved':
      return 'Approved'
    case 'review':
      return b.pr?.reviewDecision === 'CHANGES_REQUESTED' ? 'Changes requested' : 'Waiting for review'
    case 'draft':
      return 'Draft pull request'
    default:
      return STATUS_LABEL[st]
  }
}

function BranchTip({ b, now, mainRef, anchorSha, anchorSubject }: { b: Branch; now: number; mainRef: string; anchorSha: string; anchorSubject: string }) {
  const last = relTime(b.lastCommitAt, now)
  const failing = b.pr?.failingChecks ?? []
  return (
    <>
      <div className="tip-title mono">{b.name}</div>
      {b.pr ? (
        <div className="tip-line">
          <span className="muted">#{b.pr.number}</span> {b.pr.title}
        </div>
      ) : (
        <div className="tip-line muted">No pull request</div>
      )}
      <div className="tip-line">
        <span className={`st-word st-${statusOf(b)}`}>{statusDetail(b)}</span>
        {b.pr?.checks === 'pending' && <span className="muted"> · checks running</span>}
      </div>
      {failing.length > 0 && (
        <ul className="tip-list">
          {failing.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
      )}
      <div className="tip-line">
        {b.ahead} ahead · {b.behind === 0 ? `up to date with ${mainRef}` : `${b.behind} behind ${mainRef}`}
      </div>
      {b.parent ? (
        <div className="tip-line muted">
          Stacked on <span className="mono">{b.parent}</span>
        </div>
      ) : (
        anchorSha && (
          <div className="tip-line muted">
            Based on {anchorSha} {anchorSubject}
          </div>
        )
      )}
      {(b.unpushed > 0 || b.unpulled > 0) && (
        <div className="tip-line">
          {[b.unpushed > 0 && `${b.unpushed} unpushed`, b.unpulled > 0 && `${b.unpulled} not pulled`].filter(Boolean).join(' · ')}
        </div>
      )}
      {b.worktree && (
        <div className="tip-line">
          <span className="muted">Worktree </span>
          <span className="mono break">{b.worktree.path}</span>
          {b.worktree.dirty && <span> · uncommitted changes</span>}
        </div>
      )}
      {last && <div className="tip-line muted">Last commit {last}</div>}
      {b.hidden && <div className="tip-line muted">Hidden: {b.hiddenReason || 'by a hide rule'}</div>}
    </>
  )
}
