# Show measured context categories in Theia

The composer currently reads bridge totals without category counts. Prefer the task context endpoint, display measured categories and measurement age, and retain current behavior for daemons without this route.

## Scope

Frontend only. Theia consumes the daemon event stream and will refresh on selected task context changes, popover open and the existing 15 second cadence. Reads remain scoped to the selected task/session and reject superseded results.

## Abstractions

- Task context measurement carries session identity, live authority, measured time, window usage and nullable category counts.
- Context snapshot projects that measurement into the existing composer display, separately from account quotas and historical role totals.
- Composer context owns cancellable reads and category/age rendering.
- Workspace events deliver context invalidation to that owner.

## Non-goals

No daemon, store, API, TUI or macOS changes. TUI category UI is a separate ticket. Native measured context parity remains the named native-workspace-rebuild follow-up.
