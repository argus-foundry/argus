import { expect, test } from 'claude-code/testing'
import { DRAFT_DEBOUNCE_MS, DraftSync, MAX_DRAFT_CHARS, RESTORE_DELAYS_MS, restoreDraft, type RestoreEnv } from '../hooks/draft'

function syncWorld() {
  const reports: Record<string, unknown>[] = []
  let now = 0
  const timers: { at: number; fn: () => void; live: boolean }[] = []
  const sync = new DraftSync({
    report: async fields => { reports.push(fields) },
    after: (ms, fn) => {
      const t = { at: now + ms, fn, live: true }
      timers.push(t)
      return { cancel: () => { t.live = false } }
    },
  })
  const advance = (ms: number) => {
    now += ms
    for (const t of timers) if (t.live && t.at <= now) { t.live = false; t.fn() }
  }
  return { sync, reports, advance }
}

test('typing is reported once after the debounce with the latest text', () => {
  const w = syncWorld()
  w.sync.change('f', 1)
  w.advance(100)
  w.sync.change('fi', 2)
  w.advance(100)
  w.sync.change('fix', 3)
  w.advance(DRAFT_DEBOUNCE_MS - 1)
  expect(w.reports).toEqual([])
  w.advance(1)
  expect(w.reports).toEqual([{ text: 'fix', cursor: 3 }])
})

test('an emptied box is reported immediately and cancels a pending report', () => {
  const w = syncWorld()
  w.sync.change('hello', 5)
  w.advance(DRAFT_DEBOUNCE_MS)
  w.sync.change('hello!', 6)
  w.sync.change('', 0)
  expect(w.reports).toEqual([{ text: 'hello', cursor: 5 }, { text: '', cursor: 0 }])
  w.advance(DRAFT_DEBOUNCE_MS * 2)
  expect(w.reports.length).toBe(2)
})

test('a fresh session never emits a spurious empty report', () => {
  const w = syncWorld()
  w.sync.change('', 0)
  w.sync.change('', 0)
  w.advance(1000)
  expect(w.reports).toEqual([])
})

test('an unchanged draft is not re-sent, and typing back to the reported text sends nothing', () => {
  const w = syncWorld()
  w.sync.change('abc', 3)
  w.advance(DRAFT_DEBOUNCE_MS)
  w.sync.change('abc', 3)
  w.advance(DRAFT_DEBOUNCE_MS)
  w.sync.change('abcd', 4)
  w.sync.change('abc', 3)
  w.advance(DRAFT_DEBOUNCE_MS)
  expect(w.reports).toEqual([{ text: 'abc', cursor: 3 }])
})

test('oversized drafts are truncated and the cursor clamped', () => {
  const w = syncWorld()
  w.sync.change('x'.repeat(MAX_DRAFT_CHARS + 50), MAX_DRAFT_CHARS + 50)
  w.advance(DRAFT_DEBOUNCE_MS)
  const sent = w.reports[0] as { text: string; cursor: number }
  expect(sent.text.length).toBe(MAX_DRAFT_CHARS)
  expect(sent.cursor).toBe(MAX_DRAFT_CHARS)
})

test('noteReported suppresses the duplicate and drops a pending report', () => {
  const w = syncWorld()
  w.sync.change('restored', 8)
  w.sync.noteReported('restored')
  w.advance(DRAFT_DEBOUNCE_MS)
  expect(w.reports).toEqual([])
  w.sync.change('restored', 8)
  w.advance(DRAFT_DEBOUNCE_MS)
  expect(w.reports).toEqual([])
  w.sync.change('', 0)
  expect(w.reports).toEqual([{ text: '', cursor: 0 }])
})

function restoreWorld(opts: { box?: string[]; refusals?: (string | undefined)[] } = {}) {
  const box = [...(opts.box ?? [''])]
  const refusals = [...(opts.refusals ?? [undefined])]
  const filled: { text: string; cursor: number }[] = []
  const slept: number[] = []
  const reports: { text: string; cursor: number; outcome: string }[] = []
  let current = ''
  const env: RestoreEnv = {
    read: async () => ({ text: box.length > 1 ? (box.shift() as string) : (box[0] as string), cursor: 0 }),
    fill: async (text, cursor) => {
      filled.push({ text, cursor })
      const refusal = refusals.length > 1 ? refusals.shift() : refusals[0]
      if (refusal === undefined) { box[0] = text; current = text; return { isFilled: true } }
      return { isFilled: false, refusal }
    },
    sleep: async ms => { slept.push(ms) },
    report: async (text, cursor, outcome) => { reports.push({ text, cursor, outcome }); void current },
  }
  return { env, filled, slept, reports }
}

test('restore fills an empty box and reports the outcome with the box contents', async () => {
  const w = restoreWorld()
  const outcome = await restoreDraft(w.env, { text: 'fix the bug', cursor: 11 })
  expect(outcome).toBe('restored')
  expect(w.filled).toEqual([{ text: 'fix the bug', cursor: 11 }])
  expect(w.reports).toEqual([{ text: 'fix the bug', cursor: 0, outcome: 'restored' }])
})

test('restore never overwrites a box that already has text', async () => {
  const w = restoreWorld({ box: ['typed first'] })
  expect(await restoreDraft(w.env, { text: 'old', cursor: 3 })).toBe('skipped_nonempty')
  expect(w.filled).toEqual([])
  expect(w.reports[0]).toMatchObject({ text: 'typed first', outcome: 'skipped_nonempty' })
})

test('restore retries while the composer is not mounted or a dialog owns the keys', async () => {
  const w = restoreWorld({ refusals: ['no_composer', 'dialog', undefined] })
  expect(await restoreDraft(w.env, { text: 'draft', cursor: 5 })).toBe('restored')
  expect(w.filled.length).toBe(3)
  expect(w.slept).toEqual([500, 1000])
})

test('restore stops at once on a refusal that retrying cannot fix', async () => {
  const w = restoreWorld({ refusals: ['hook-said-no'] })
  expect(await restoreDraft(w.env, { text: 'draft', cursor: 5 })).toBe('refused:hook-said-no')
  expect(w.filled.length).toBe(1)
})

test('restore gives up after the last delay when the composer never mounts', async () => {
  const w = restoreWorld({ refusals: ['no_composer'] })
  expect(await restoreDraft(w.env, { text: 'draft', cursor: 5 })).toBe('refused:no_composer')
  expect(w.filled.length).toBe(RESTORE_DELAYS_MS.length)
})

test('restore clamps an out-of-range cursor', async () => {
  const w = restoreWorld()
  await restoreDraft(w.env, { text: 'abc', cursor: 99 })
  expect(w.filled[0]).toEqual({ text: 'abc', cursor: 3 })
  const n = restoreWorld()
  await restoreDraft(n.env, { text: 'abc', cursor: -4 })
  expect(n.filled[0]).toEqual({ text: 'abc', cursor: 0 })
})
