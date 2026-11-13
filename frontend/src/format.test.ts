import { describe, expect, it } from 'vitest'
import { baseName, plural, relTime } from './format'

const now = Date.parse('2026-11-13T17:00:00Z')
const ago = (sec: number) => new Date(now - sec * 1000).toISOString()

describe('relTime', () => {
  it('formats recent times', () => {
    expect(relTime(ago(10), now)).toBe('just now')
    expect(relTime(ago(120), now)).toBe('2m ago')
    expect(relTime(ago(59 * 60), now)).toBe('59m ago')
    expect(relTime(ago(3 * 3600 + 100), now)).toBe('3h ago')
    expect(relTime(ago(5 * 86400), now)).toBe('5d ago')
    expect(relTime(ago(100 * 86400), now)).toBe('3mo ago')
    expect(relTime(ago(800 * 86400), now)).toBe('2y ago')
  })

  it('treats the Go zero time and bad input as unknown', () => {
    expect(relTime('0001-01-01T00:00:00Z', now)).toBeNull()
    expect(relTime('', now)).toBeNull()
    expect(relTime('nope', now)).toBeNull()
  })

  it('clamps future times', () => {
    expect(relTime(ago(-300), now)).toBe('just now')
  })
})

describe('helpers', () => {
  it('pluralises', () => {
    expect(plural(1, 'commit')).toBe('1 commit')
    expect(plural(3, 'commit')).toBe('3 commits')
    expect(plural(2, 'branch', 'branches')).toBe('2 branches')
  })

  it('takes base names', () => {
    expect(baseName('/home/dev/my-app')).toBe('my-app')
    expect(baseName('C:\\src\\repo\\')).toBe('repo')
  })
})
