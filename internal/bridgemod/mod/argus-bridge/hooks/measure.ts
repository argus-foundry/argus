// session.measure → wire fields, plus the throttle that keeps a chatty event
// (it fires after every tool step) from flooding the daemon.

export type MeasureInput = {
  context?: { tokens?: number; percent?: number; window?: number }
  rateLimits?: ReadonlyArray<{ kind: string; percentUsed: number; resetsAt?: string }>
  cost?: { usd: number }
}

export type BreakdownRow = { name: string; tokens: number; kind: string }

// The engine labels breakdown rows as /context prints them and types only
// used/free/buffer/deferred, so a row maps by its label and an unknown label
// stays unmapped (null on the wire) rather than guessed.
const CATEGORY_BY_LABEL: Record<string, string> = {
  'system prompt': 'system',
  'system tools': 'tools',
  'mcp tools': 'tools',
  'custom agents': 'agents',
  'memory files': 'memory',
  skills: 'skills',
  messages: 'messages',
}

/** Sums used rows into the wire categories; the compaction reserve is the buffer row. */
export function categoriesFromBreakdown(rows: ReadonlyArray<BreakdownRow> | undefined): Record<string, number> | undefined {
  if (!rows) return undefined
  const out: Record<string, number> = {}
  for (const r of rows) {
    if (typeof r.tokens !== 'number' || !Number.isFinite(r.tokens) || r.tokens < 0) continue
    const key = r.kind === 'buffer' ? 'compaction' : r.kind === 'used' ? CATEGORY_BY_LABEL[r.name.trim().toLowerCase()] : undefined
    if (key) out[key] = (out[key] ?? 0) + r.tokens
  }
  return Object.keys(out).length ? out : undefined
}

const MAX_SUGGESTION_BYTES = 4096

/** The wire fields of a suggestion.changed event, or undefined when the text must not be reported. */
export function suggestionFields(text: string): { text: string } | undefined {
  if (typeof text !== 'string' || text.trim() === '' || text.includes('\0')) return undefined
  let bytes = 0
  for (const ch of text) {
    const c = ch.codePointAt(0) as number
    bytes += c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4
    if (bytes > MAX_SUGGESTION_BYTES) return undefined
  }
  return { text }
}

/** Only text the engine itself proposed and the prompt box can show is the provider's suggestion. */
export function suggestionToReport(origin: { kind: string }, isShown: boolean, text: string): { text: string } | undefined {
  return origin.kind === 'suggestion' && isShown ? suggestionFields(text) : undefined
}

export function measureFields(m: MeasureInput, categories?: Record<string, number>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  if (m.context) {
    out.context = { tokens: m.context.tokens, percent: m.context.percent, window: m.context.window, categories }
  }
  if (m.cost) out.cost_usd = m.cost.usd
  out.rate_limits = (m.rateLimits ?? []).map(r => ({
    kind: r.kind,
    percent_used: r.percentUsed,
    resets_at: r.resetsAt,
  }))
  return out
}

export const MEASURE_MIN_INTERVAL_MS = 5000

/** Holds the latest measurement; releases it at most once per interval, or on demand. */
export class MeasureThrottle {
  private latest: Record<string, unknown> | undefined
  private lastSentAt = -Infinity
  private lastKey = ''

  /** Records a measurement; returns it when it should be sent now. */
  offer(fields: Record<string, unknown>, now: number): Record<string, unknown> | undefined {
    this.latest = fields
    return now - this.lastSentAt >= MEASURE_MIN_INTERVAL_MS ? this.take(now) : undefined
  }

  /** Releases the held measurement if it differs from what was last sent. */
  flush(now: number): Record<string, unknown> | undefined {
    return this.latest ? this.take(now) : undefined
  }

  private take(now: number): Record<string, unknown> | undefined {
    const fields = this.latest
    this.latest = undefined
    if (!fields) return undefined
    const key = JSON.stringify(fields)
    if (key === this.lastKey) return undefined
    this.lastKey = key
    this.lastSentAt = now
    return fields
  }
}
