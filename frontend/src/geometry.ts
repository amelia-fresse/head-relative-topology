// Pixel geometry of the map, per density. Themes never change these (only
// colours, fonts and radii), so lines stay aligned in every theme.
//
// All x values are measured from the row's left edge, y values from the row's
// top. A row's "dot line" (dotY) is where its dot, straight connector and
// elbow ends sit. In normal mode dotY must match the vertical centre of the
// card title: row padding-top + card border + card padding-top + title
// line-height / 2 (see .density-normal rules in app.css).

export interface Geometry {
  /** Centre of the main line. */
  mainX: number
  mainW: number
  laneW: number
  /** Shared junction lane for siblings of one anchor. */
  junctionX: number
  /** Dot centre of a main-based branch (depth 0). */
  dotX0: number
  /** Extra indent per stacking level. */
  stackStep: number
  /** Deeper stacks are drawn at this depth to keep cards readable at 320px. */
  maxDepth: number
  dotY: number
  dotSize: number
  headDotSize: number
  anchorDotSize: number
  elbowR: number
  /** Merged rows: anchor dot on main (when the row carries it) and the PR dot. */
  mergeAnchorY: number
  mergeDotY: number
  mergeR: number
  mergedDotSize: number
  mergedRowH: number
  gapRowH: number
  tailRowH: number
  /** Gap between a dot's edge and its card / label. */
  contentGap: number
  /** Where the HEAD card / chip starts (clear of the junction lane). */
  headContentX: number
  junctionStubR: number
}

export const NORMAL: Geometry = {
  mainX: 16,
  mainW: 4,
  laneW: 2,
  junctionX: 30,
  dotX0: 78,
  stackStep: 44,
  maxDepth: 3,
  dotY: 23,
  dotSize: 14,
  headDotSize: 18,
  anchorDotSize: 12,
  elbowR: 10,
  mergeAnchorY: 10,
  mergeDotY: 28,
  mergeR: 8,
  mergedDotSize: 10,
  mergedRowH: 40,
  gapRowH: 26,
  tailRowH: 30,
  contentGap: 8,
  headContentX: 42,
  junctionStubR: 8,
}

export const COMPACT: Geometry = {
  mainX: 14,
  mainW: 4,
  laneW: 2,
  junctionX: 26,
  dotX0: 60,
  stackStep: 34,
  maxDepth: 3,
  dotY: 16,
  dotSize: 10,
  headDotSize: 14,
  anchorDotSize: 10,
  elbowR: 8,
  mergeAnchorY: 8,
  mergeDotY: 21,
  mergeR: 6,
  mergedDotSize: 8,
  mergedRowH: 30,
  gapRowH: 20,
  tailRowH: 22,
  contentGap: 8,
  headContentX: 36,
  junctionStubR: 6,
}

export function dotX(g: Geometry, depth: number): number {
  return g.dotX0 + Math.min(depth, g.maxDepth) * g.stackStep
}

export function laneX(g: Geometry, col: 'junction' | number): number {
  return col === 'junction' ? g.junctionX : dotX(g, col)
}

/** Left edge of a branch's card (normal) or name (compact). */
export function contentX(g: Geometry, depth: number): number {
  return dotX(g, depth) + g.dotSize / 2 + g.contentGap
}
