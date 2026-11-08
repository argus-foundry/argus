## Why

Follow-up to quiet-system-notices (#1167), per coordinator decision: the legacy system-note backlog is all stale, so the cleanup must collapse it to the newest per (task, type) AND mark the survivors read, and it must run exactly once (an every-start run would mark brand-new unread notes read, defeating "unread but uncounted").

## What Changes

- The startup cleanup becomes a one-shot, marker-guarded migration (`migration.system_notices_v1` config key, set only after success) that collapses the backlog and marks survivors read.
- The `argus:system` sender predicate is a single shared SQL constant used by the unread count, the inbox-full cap and the `ExcludeSystem` filter.

## Capabilities

### New Capabilities

### Modified Capabilities
- `task-messaging`: coalescing requirement's startup backlog cleanup is one-shot and marks survivors read.

## Impact

`internal/db/messages.go`, `internal/db/migrate.go`, tests. Daemon-only; no frontend, mod or web-shell change. **Frontend parity (TUI, web, macOS):** no client-visible change beyond the already-merged exclusion; no named gaps.
