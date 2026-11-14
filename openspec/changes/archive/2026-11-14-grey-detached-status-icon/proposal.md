# Grey out the status icon of a detached agent

## Why

After a supervisor restart, finished workers and idle PR-waiting workers are deliberately not resumed, so they have no live session. The Hera rail marks such a role "disconnected" with a hollow `○`. That glyph outranks the accepted / ready_to_close / done icons, so a finished worker with an open PR looks identical to one that died mid-work. Status, PR meta and ready_to_close are all still in the DB; the icon only masks them.

## What Changes

- A detached role keeps its **normal status icon** (working / needs-input / ready-to-close / accepted / done / failed / idle / live), rendered **grey**. The `○` disconnected glyph is removed entirely.
- This applies to every detached role regardless of state. A spinner-type "working" icon on a detached role renders as a **static** grey glyph (no animation).
- The details-pane roster label keeps the real state and appends the detached marker, e.g. `ready · detached` (replacing the bare `disconnected` label).
- The TUI Tasks tab applies the same grey treatment: its status glyph (needs-input / idle / review / complete / spinner) renders grey for a detached task, spinner static.
- Detection is unchanged: in_progress/in_review, not archived, missing from the live-session list for 2 ticks, fail-open on an unreadable list. Only rendering changes.

## Capabilities

### Modified Capabilities

- `hera-view`: replaces "Rail shows a disconnected glyph…" with a grey-icon requirement; roster label.
- `task-list-view`: per-task status indicator greys for a detached task.

## Non-Goals

- **Web SPA and macOS app are NOT changed.** They would need the daemon to expose a `detached` flag over REST (today it is derived in TUI memory only). Named follow-ups: expose a detached flag on the task/hera REST surface, then grey the icon in the web SPA and the macOS app.
- No change to detection logic or grace ticks.
- Out of scope (separate bug): daemon-only restarts re-record already-dead in_review workers as fresh orphans, resetting `killed_at` and re-sending ARGUS_BOUNCED.
