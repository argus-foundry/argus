## Context

Board read state and authored revisions already support human revision-checked REST mutations. All clients share the durable board and attention summaries.

## Goals / Non-Goals

**Goals:** Any open item can be deferred and restored; hidden items keep authored updates and stop demanding attention. TUI and web expose explicit controls and counts.

**Non-Goals:** Coordinator-owned deferral, timed reminders, automatic restoration and persistence of visibility preferences. macOS remains read-only; named follow-up: macOS deferred-board visibility toggle and human mutation parity.

## Decisions

- Store nullable `deferred_at` separately from `updated_at`; human deferral must not invalidate authored response fingerprints or erase unread state. Repeated set operations are idempotent.
- Use revision-checked `PUT .../defer` with an explicit boolean. Reuse board REST validation and post-lock event emission. Add no MCP mutation; list serialization includes the timestamp.
- Keep full board snapshots so clients can compute deferred counts and toggle locally; apply attention suppression in shared DB/model predicates and hide deferred entries in client filters.
- Use `d` for defer/restore and `D` for show/hide deferred in the focused TUI board, routed through the keymap.
- Label trigger-pending and non-blocking work “later” to reserve “deferred” for human intent.

## Risks / Trade-offs

- Hidden authored updates could be forgotten → retain unread state and a visible deferred count.
- Client-specific attention checks could diverge → cover DB summaries, model attention and web Decide filtering with regression tests.
- Stale clicks could affect changed content → require the displayed authored revision and return conflict without writing.
