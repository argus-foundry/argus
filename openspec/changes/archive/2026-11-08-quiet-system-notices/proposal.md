## Why

The daemon posts poll-only system notes (`ARGUS_HOST_SUSPENDED`, `ARGUS_BOUNCED`) into task inboxes. Nothing pushes them and agents never poll for them, so they pile up: 2755 unread across 82 tasks on 2026-11-08. They show as "N unread from argus:system" in the Claude bridge band (read as "tell Claude to read its mail") and count toward the 500-unread cap, so a long-lived task would eventually reject real messages and `task_ask` replies.

## What Changes

- System notes (sender `argus:system`) are **not unread mail**: excluded from every human-facing unread count (bridge band and `/hera-inbox`, REST inbox `unread_count`, TUI `i` inbox), so TUI, web and macOS agree. They stay stored and readable via `task_inbox`.
- **Coalescing:** a new system note replaces the task's previous unread note of the same type.
- **Cap:** system notes do not count toward `MaxUnreadPerRecipient`, and system inserts never fail with inbox-full.
- **Cleanup:** idempotent startup migration collapses the existing backlog to the newest unread note per (task, type).
- Host-suspend notice is kept (decision from coordinator); no consumer is added.

## Capabilities

### New Capabilities

### Modified Capabilities
- `task-messaging`: message caps exclude system notes; system notes are not counted as unread mail; system notes coalesce per type.
- `daemon-lifecycle`: host-suspend and bounce notes coalesce per task.
- `argus-bridge`: band/inbox command unread excludes system notes.

## Impact

`internal/db/messages.go`, `internal/db/migrate.go`, `internal/api/bridge_ui.go`, `internal/api/messages.go`, `internal/tui/inbox.go`. Daemon-only: the bridge mod, supervisor spawn digest and web shell assets are unchanged.

**Frontend parity (TUI, web, macOS):** the fix is at the REST/DB layer. TUI `i` modal marks system rows not-unread; web has no inbox UI (nothing to change); macOS decodes `unread_count` from the same endpoint and picks up the corrected value with no client change. No named gaps.
