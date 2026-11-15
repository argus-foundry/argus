## Why

Playwright launches Chrome in a detached process group and its MCP isolated launcher disables Playwright SIGTERM handling. Session group cleanup therefore leaves browser processes behind, including processes reparented to launchd.

## What changes

- Stop positively identified task-owned Playwright browsers and their captured helpers on full session stop and worktree removal, with TERM then bounded KILL escalation.
- Sweep browsers belonging to finished or missing tasks after a grace period, preserving live sessions and failing closed on uncertain ownership.
- Add a read-only doctor advisory and document the investigation and safety constraints.

## Capabilities

### New capabilities

- `task-browser-cleanup`: ownership checks, lifecycle cleanup, orphan sweep and doctor advisory.

### Modified capabilities

None.

## Impact

Agent stop/removal, daemon lifecycle sweep and doctor. No REST contract or frontend controls change; TUI, web and macOS benefit through the shared daemon. No browser launch defaults or sandbox grants change.
