## Context

The daemon already gates a resize-driven kick on the Claude mod's draft report (`bridgeDraftPending`) and invalidates its per-task cols cache on a deferral, so a later same-size `/resize` re-evaluates. Clients only need to say what the daemon cannot see and to re-POST.

## Decisions

- **Request fields, not a new endpoint.** `scrolled_up` and `composer_dirty` ride the existing `/resize` body (omitted = false). The response adds `deferred` so a client knows it owes a retry. Additive JSON; no versioning.
- **Gate order = the TUI's** (`agent.ComposerDraft`, then scroll): mod report, then the daemon's `ScreenRenderer.InputDraft` on the session's output tail (found=false is "unknown", falls through), then `composer_dirty`. Draft outranks scroll; scroll is uncapped; the flag is capped at `agent.KickDeferCap` per task+width via a small `Server` clock (started at first deferral, cleared by any non-deferral outcome).
- **Screen read only on the evaluation path.** It runs after the margin, idle and prompt gates have already said "this would kick" and only when no draft-aware bridge answered, so steady-state resizes and the unchanged-cols short circuit never pay for it.
- **Setting off** ignores both fields and skips the screen read.
- **Web flag** is a minimal pure JS function (same fold as `agentview.ComposerTyped`: printable/paste set, CR/Ctrl+C/Ctrl+U clear, escapes/other controls/DEL ignored), reset after a compose-bar submit. Compose-bar text is not in the agent box until sent, so it is not a draft.
- **Retry** is client-driven: on `deferred != ""` arm a 10 s timer plus a scroll-to-bottom trigger; each firing re-POSTs the current cols/rows; stop when a response has `deferred == ""`, a kick lands, the task changes, the page is hidden or the terminal view is torn down.
- **macOS** mirrors the web with SwiftTerm's scroll position and a pure Swift fold in ArgusKit (testable under `make mac-test`).

## Non-Goals / limits

- No SPA JS unit harness is built here; the JS is kept minimal and pure.
- Typing in another client or through another handle is invisible to the flag (same known limitation as the TUI).
- Attachment restore and non-Claude restore remain out of scope as in #1158.
- GUI paths not verifiable offline: real xterm.js/SwiftTerm scroll signals and the retry timers end to end.
