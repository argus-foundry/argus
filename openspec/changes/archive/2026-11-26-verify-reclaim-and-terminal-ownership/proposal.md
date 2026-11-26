## Why

Cleanup can encounter a replaced worktree, and terminal snapshots can contain recorded bytes without a live process identity. Both operations need to verify their current owner before acting.

## What Changes

- Revalidate Git ownership before resident or stacked cleanup.
- Keep browser actions and controls scoped to the detail visit that started them.
- Keep PID-less recorded output separate from the replacement stream in workspace and native macOS.

## Impact

Cleanup follows shared daemon task and Git ownership. Browser and native terminal clients share the snapshot contract. The native TUI already switches replay when it sees a replacement PID.
