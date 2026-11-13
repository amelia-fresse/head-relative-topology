// Rails draws the line segments that belong to one row: the main line above
// and below the row's split point, lanes passing through, the row's own
// connector and the anchor dot on main. Every piece is an absolutely
// positioned element inside the row, so lines scroll with the content and
// stretch with rows whose height depends on wrapped text.
import type { CSSProperties, ReactElement } from 'react'
import { dotX, laneX, type Geometry } from '../geometry'
import type { Connector, Lane, MainDot, MainSegs } from '../layout'

interface Props {
  g: Geometry
  main: MainSegs
  lanes: Lane[]
  connector: Connector
  mainDot: MainDot | null
  /** y of the anchor dot on main / where the main segments meet. */
  splitY: number | string
  /** y of the row's own dot (branch, merged PR). */
  dotY: number
  /** Depth of the row's own dot (0 for merged PRs). */
  depth: number
  /** Extra class for the connector (e.g. dimmed hidden branches). */
  tone?: 'normal' | 'dim'
}

const px = (v: number | string) => (typeof v === 'number' ? `${v}px` : v)

export function Rails({ g, main, lanes, connector, mainDot, splitY, dotY, depth, tone = 'normal' }: Props) {
  const x = dotX(g, depth)
  const half = g.laneW / 2
  const parts: ReactElement[] = []

  // Lanes first (below the main line).
  lanes.forEach((l, i) => {
    const lx = laneX(g, l.col)
    if (l.seg === 'full') {
      parts.push(<i key={`l${i}`} className="seg lane" style={{ left: lx - half, top: 0, bottom: 0, width: g.laneW }} />)
    } else if (l.col === 'junction') {
      // Rounded stub from the anchor dot on main out to the junction, then down.
      parts.push(
        <i
          key={`l${i}`}
          className="seg junction-start"
          style={{
            left: g.mainX,
            top: dotY - half,
            width: g.junctionX - g.mainX + half,
            bottom: 0,
            borderTopRightRadius: g.junctionStubR,
          }}
        />,
      )
    } else {
      parts.push(<i key={`l${i}`} className="seg lane" style={{ left: lx - half, top: dotY, bottom: 0, width: g.laneW }} />)
    }
  })

  // The row's own connector.
  if (connector.kind === 'straight') {
    parts.push(
      <i key="c" className={`seg lane ${tone}`} style={{ left: g.mainX, top: dotY - half, width: x - g.mainX, height: g.laneW }} />,
    )
  } else if (connector.kind === 'elbow') {
    const fx = laneX(g, connector.from)
    parts.push(
      <i
        key="c"
        className={`seg elbow ${tone}`}
        style={{ left: fx - half, top: 0, width: x - fx + half, height: dotY + half, borderBottomLeftRadius: fx === x ? 0 : g.elbowR }}
      />,
    )
  } else if (connector.kind === 'merge') {
    const fromY = mainDot ? g.mergeAnchorY : 0
    parts.push(
      <i
        key="c"
        className="seg merge-elbow"
        style={{ left: g.mainX - half, top: fromY, width: x - g.mainX + half, height: g.mergeDotY - fromY + half, borderBottomLeftRadius: g.mergeR }}
      />,
    )
  }

  // Main line on top of lanes and connectors.
  const mainStyle = (seg: string): string => `seg main-${seg}`
  const mw = (seg: string) => (seg === 'link' ? g.laneW : g.mainW)
  if (main.top) {
    parts.push(
      <i key="mt" className={mainStyle(main.top)} style={{ left: g.mainX - mw(main.top) / 2, width: mw(main.top), top: 0, height: px(splitY) } as CSSProperties} />,
    )
  }
  if (main.bottom) {
    parts.push(
      <i key="mb" className={mainStyle(main.bottom)} style={{ left: g.mainX - mw(main.bottom) / 2, width: mw(main.bottom), top: px(splitY), bottom: 0 } as CSSProperties} />,
    )
  }

  if (mainDot) {
    const size = mainDot === 'head' ? g.headDotSize : g.anchorDotSize
    parts.push(
      <i
        key="md"
        className={`anchor-dot anchor-${mainDot}`}
        style={{ left: g.mainX - size / 2, top: `calc(${px(splitY)} - ${size / 2}px)`, width: size, height: size }}
      />,
    )
  }

  return (
    <span className="rails" aria-hidden="true">
      {parts}
    </span>
  )
}
