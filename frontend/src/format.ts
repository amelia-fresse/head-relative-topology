// Small pure formatting helpers.

/** Parses a Go time.Time JSON value; returns null for the zero time or garbage. */
export function parseTime(t: string | null | undefined): Date | null {
  if (!t) return null
  const d = new Date(t)
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return null
  return d
}

/** "just now", "2m ago", "3h ago", "5d ago", "4mo ago", "2y ago"; null for the zero time. */
export function relTime(t: string | null | undefined, now: number): string | null {
  const d = parseTime(t)
  if (!d) return null
  const s = Math.max(0, Math.round((now - d.getTime()) / 1000))
  if (s < 45) return 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return `${Math.max(1, m)}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const days = Math.floor(h / 24)
  if (days < 45) return `${days}d ago`
  const months = Math.floor(days / 30.44)
  if (months < 12) return `${months}mo ago`
  return `${Math.floor(days / 365.25)}y ago`
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}

/** Last path segment, for showing a repo when it has no GitHub slug. */
export function baseName(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean)
  return parts[parts.length - 1] ?? path
}
