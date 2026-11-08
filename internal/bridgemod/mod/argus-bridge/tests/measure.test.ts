import { expect, test } from 'claude-code/testing'
import { MEASURE_MIN_INTERVAL_MS, MeasureThrottle, categoriesFromBreakdown, measureFields, suggestionFields, suggestionToReport } from '../hooks/measure'

test('measureFields maps context, cost and every rate limit window', () => {
  expect(
    measureFields({
      context: { tokens: 1200, percent: 3, window: 200000 },
      cost: { usd: 0.5 },
      rateLimits: [{ kind: 'five_hour', percentUsed: 41, resetsAt: '2026-10-24T20:00:00Z' }, { kind: 'seven_day', percentUsed: 9 }],
    }),
  ).toEqual({
    context: { tokens: 1200, percent: 3, window: 200000, categories: undefined },
    cost_usd: 0.5,
    rate_limits: [
      { kind: 'five_hour', percent_used: 41, resets_at: '2026-10-24T20:00:00Z' },
      { kind: 'seven_day', percent_used: 9, resets_at: undefined },
    ],
  })
})

test('measureFields omits what the engine does not have', () => {
  expect(measureFields({})).toEqual({ rate_limits: [] })
})

test('the throttle sends the first measurement, holds the rest, and flush releases the latest', () => {
  const th = new MeasureThrottle()
  expect(th.offer({ n: 1 }, 0)).toEqual({ n: 1 })
  expect(th.offer({ n: 2 }, 1000)).toBe(undefined)
  expect(th.offer({ n: 3 }, 2000)).toBe(undefined)
  expect(th.flush(3000)).toEqual({ n: 3 })
  expect(th.flush(4000)).toBe(undefined)
})

test('the throttle releases again once the interval has passed', () => {
  const th = new MeasureThrottle()
  th.offer({ n: 1 }, 0)
  expect(th.offer({ n: 2 }, MEASURE_MIN_INTERVAL_MS)).toEqual({ n: 2 })
})

test('an unchanged measurement is not resent', () => {
  const th = new MeasureThrottle()
  th.offer({ n: 1 }, 0)
  expect(th.offer({ n: 1 }, MEASURE_MIN_INTERVAL_MS * 2)).toBe(undefined)
})

test('categoriesFromBreakdown maps used rows by label, sums tools, and takes the buffer as compaction', () => {
  expect(
    categoriesFromBreakdown([
      { name: 'System prompt', tokens: 1000, kind: 'used' },
      { name: 'System tools', tokens: 2000, kind: 'used' },
      { name: 'MCP tools', tokens: 500, kind: 'used' },
      { name: 'MCP tools (deferred)', tokens: 9000, kind: 'deferred' },
      { name: 'Memory files', tokens: 0, kind: 'used' },
      { name: 'Custom agents', tokens: 7, kind: 'used' },
      { name: 'Skills', tokens: 9, kind: 'used' },
      { name: 'Messages', tokens: 5000, kind: 'used' },
      { name: 'Mystery', tokens: 7, kind: 'used' },
      { name: 'Free space', tokens: 150000, kind: 'free' },
      { name: 'Autocompact buffer', tokens: 33000, kind: 'buffer' },
    ]),
  ).toEqual({ system: 1000, tools: 2500, memory: 0, agents: 7, skills: 9, messages: 5000, compaction: 33000 })
})

test('categoriesFromBreakdown drops invalid counts and answers nothing when nothing maps', () => {
  expect(categoriesFromBreakdown(undefined)).toBe(undefined)
  expect(categoriesFromBreakdown([{ name: 'Messages', tokens: -1, kind: 'used' }, { name: 'Skills', tokens: NaN, kind: 'used' }])).toBe(undefined)
})

test('deferred rows stay unknown even with a recognized category label', () => {
  expect(categoriesFromBreakdown([{ name: 'Messages', tokens: 123, kind: 'deferred' }])).toBe(undefined)
})

test('only a shown engine suggestion is reported', () => {
  expect(suggestionToReport({ kind: 'suggestion' }, true, 'Run the focused tests')).toEqual({ text: 'Run the focused tests' })
  expect(suggestionToReport({ kind: 'suggestion' }, false, 'Run the focused tests')).toBe(undefined)
  expect(suggestionToReport({ kind: 'plugin' }, true, 'Run the focused tests')).toBe(undefined)
})

test('blank, NUL and oversized suggestions are never reported', () => {
  expect(suggestionFields('   ')).toBe(undefined)
  expect(suggestionFields('a\0b')).toBe(undefined)
  expect(suggestionFields('x'.repeat(4096))).toEqual({ text: 'x'.repeat(4096) })
  expect(suggestionFields('x'.repeat(4097))).toBe(undefined)
  expect(suggestionFields('é'.repeat(2049))).toBe(undefined)
})
