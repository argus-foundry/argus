## Context

Partial submits currently force the global queue into the main pane and preserve dead choice-only drafts.

## Goals / Non-goals

Keep board/tab context and durable written feedback while preserving immutable uncertain payloads and receipt replay. TUI and macOS have no standalone Decide landing, so no client changes are needed there; existing REST contracts stay intact.

## Decisions

- Store a transient board/tab submit notice, cleared on navigation/edit. Refresh after every submit.
- Separate unanswerable written recovery from queued records; never discard uncertain payloads. Group explicit review by coordinator and render one status per item.
- Keep authored revisions stable for readiness-only updates; retain normal events and unread behavior. Material content/command/trigger/blocking changes still advance revisions.

## Risks / Trade-offs

- Refresh can fail after acceptance → preserve local per-item acceptance/counts and explain refresh failure without replaying accepted work.
- Legacy saved drafts can be closed → normalize after snapshot while preserving written notes/annotations.
