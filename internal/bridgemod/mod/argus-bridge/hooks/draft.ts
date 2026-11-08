// Composer-draft protocol (kick-timing-keep-place). Pure of the engine: it only
// touches what `DraftSyncEnv` / `RestoreEnv` hand it, so it is unit-tested with
// fakes. Neither class ever throws into a hook; callers wrap them in `safe`.

/** A typed draft is reported this long after the last keystroke. */
export const DRAFT_DEBOUNCE_MS = 300
/** Longer drafts are reported truncated: a batch must stay far below the daemon's body cap. */
export const MAX_DRAFT_CHARS = 100_000
/** Cumulative waits (ms) before each restore attempt; the box may not be mounted at session.start. */
export const RESTORE_DELAYS_MS = [0, 500, 1_500, 3_500, 7_500]

export interface Cancelable {
  cancel(): void
}

export interface DraftSyncEnv {
  /** Posts a `draft` event with these fields. */
  report(fields: Record<string, unknown>): Promise<void>
  after(ms: number, fn: () => void): Cancelable
}

/**
 * Reports the prompt-box draft to the daemon: typing is debounced, an emptied
 * box (a submit, a clear) is reported at once, and an unchanged draft is never
 * re-sent. `last` starts empty so a fresh session never emits a spurious empty
 * report that would erase a draft the daemon is about to hand back.
 */
export class DraftSync {
  private last = ''
  private pending: { text: string; cursor: number } | undefined
  private timer: Cancelable | undefined

  constructor(private readonly env: DraftSyncEnv) {}

  change(text: string, cursor: number): void {
    this.timer?.cancel()
    this.timer = undefined
    const clipped = text.length > MAX_DRAFT_CHARS ? text.slice(0, MAX_DRAFT_CHARS) : text
    if (clipped === '') {
      this.pending = undefined
      if (this.last === '') return
      this.last = ''
      void this.env.report({ text: '', cursor: 0 })
      return
    }
    this.pending = { text: clipped, cursor: Math.min(cursor, clipped.length) }
    this.timer = this.env.after(DRAFT_DEBOUNCE_MS, () => this.flush())
  }

  /** Records what an explicit report (a restore outcome) already told the daemon. */
  noteReported(text: string): void {
    this.last = text.length > MAX_DRAFT_CHARS ? text.slice(0, MAX_DRAFT_CHARS) : text
    this.pending = undefined
    this.timer?.cancel()
    this.timer = undefined
  }

  private flush(): void {
    this.timer = undefined
    const p = this.pending
    this.pending = undefined
    if (!p || p.text === this.last) return
    this.last = p.text
    void this.env.report({ text: p.text, cursor: p.cursor })
  }
}

export interface RestoreEnv {
  read(): Promise<{ text: string; cursor: number }>
  fill(text: string, cursor: number): Promise<{ isFilled: boolean; refusal?: string }>
  sleep(ms: number): Promise<void>
  /** Tells the daemon how the restore ended and what the box now holds. */
  report(text: string, cursor: number, outcome: string): Promise<void>
}

export type RestoreOutcome = 'restored' | 'skipped_nonempty' | `refused:${string}`

/**
 * Puts a handed-back draft into the resumed session's empty box. It never
 * overwrites: a box that already holds text (the user typed first, or a hot
 * reload kept the old text) wins. `no_composer` / `dialog` refusals are retried
 * on RESTORE_DELAYS_MS because the box mounts after session.start and a
 * permission dialog can briefly own the keys.
 */
export async function restoreDraft(env: RestoreEnv, draft: { text: string; cursor: number }, delays: readonly number[] = RESTORE_DELAYS_MS): Promise<RestoreOutcome> {
  const cursor = Math.max(0, Math.min(draft.cursor, draft.text.length))
  let refusal = 'unknown'
  let waited = 0
  let outcome: RestoreOutcome | undefined
  for (const at of delays) {
    if (at > waited) {
      await env.sleep(at - waited)
      waited = at
    }
    const box = await env.read()
    if (box.text !== '') {
      outcome = 'skipped_nonempty'
      break
    }
    const result = await env.fill(draft.text, cursor)
    if (result.isFilled) {
      outcome = 'restored'
      break
    }
    refusal = result.refusal ?? 'hook'
    if (refusal !== 'no_composer' && refusal !== 'dialog') break
  }
  outcome ??= `refused:${refusal}`
  const after = await env.read()
  await env.report(after.text, after.cursor, outcome)
  return outcome
}
