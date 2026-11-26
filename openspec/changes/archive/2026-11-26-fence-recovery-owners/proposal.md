## Why

An existing Hera binding history can outlive the operation that wrote it. Terminal detail refresh can also run while recovery owns an attachment. Both paths need current ownership to finish safely.

## What Changes

- Validate legacy reclaim evidence and protect a pending graduation even for a hidden role.
- Preserve resident worker worktrees until the reclaim owns the task and the outgoing session has retired.
- Keep terminal recovery in charge of reconnect attempts and preserve tab selection through reauthentication.

## Impact

Hera uses shared task state across TUI, REST, and MCP. The terminal page change applies to the PWA. Native macOS uses its own terminal controller and has no PWA tab or authentication view.
