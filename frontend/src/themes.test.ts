import { describe, expect, it } from 'vitest'
import css from './themes.css?raw'
import { THEMES } from './model'

function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16)
  const ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
    const c = v / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2]
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

function themeVars(name: string): Record<string, string> {
  const re = new RegExp(`:root\\[data-theme='${name}'\\]\\s*\\{([^}]*)\\}`)
  const block = css.match(re)?.[1]
  if (!block) throw new Error(`theme ${name} not found`)
  const vars: Record<string, string> = {}
  for (const m of block.matchAll(/--([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) vars[m[1]] = m[2]
  return vars
}

// Text tokens and the backgrounds they are drawn on.
const TEXT_ON_GROUND_AND_SURFACE = ['text', 'muted', 'merged', 'st-approved', 'st-review', 'st-ci-failing', 'st-conflicts', 'st-draft']
const PAIRS: [string, string][] = [
  ['head-fg', 'head-bg'],
  ['head-meta', 'head-bg'],
  ['pill-fg', 'pill-bg'],
  ['count-fg', 'count-bg'],
  ['btn-primary-fg', 'btn-primary-bg'],
  ['text', 'tip-bg'],
  ['muted', 'tip-bg'],
  ['text', 'surface-hover'],
  ['muted', 'surface-hover'],
]

describe.each(THEMES)('theme %s', (theme) => {
  const v = themeVars(theme)

  it.each(TEXT_ON_GROUND_AND_SURFACE)('--%s reaches 4.5:1 on ground and surface', (token) => {
    expect(v[token], `missing --${token}`).toBeDefined()
    expect(contrast(v[token], v.ground)).toBeGreaterThanOrEqual(4.5)
    expect(contrast(v[token], v.surface)).toBeGreaterThanOrEqual(4.5)
  })

  it.each(PAIRS)('--%s reaches 4.5:1 on --%s', (fg, bg) => {
    expect(v[fg], `missing --${fg}`).toBeDefined()
    expect(v[bg], `missing --${bg}`).toBeDefined()
    expect(contrast(v[fg], v[bg])).toBeGreaterThanOrEqual(4.5)
  })
})
