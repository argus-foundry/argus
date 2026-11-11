## Why

New Claude sessions and their sub-agents stop at folder trust dialogs in Argus-created worktrees. Argus already owns these checkouts and should prepare trust before launch.

## What changes

- Seed Claude folder trust by default before starting each new worktree task, using its resolved account.
- Add `[claude] trust_worktrees = false` as the config.toml opt-out.
- Preserve unrelated JSON values, atomically replace the config, and log failures without blocking creation.

## Capabilities

### New capabilities

- `claude-worktree-trust`: Account-aware, best-effort folder trust for every shared worktree creation path.

### Modified capabilities

None.

## Impact

- Shared task creation, backend family registry, config defaults, and Claude JSON injection.
- TUI, web and macOS task creation inherit the same daemon behavior; no wire or client UI change.
- Follow-up: remove stale trust entries on worktree deletion after defining account-aware cleanup ownership. Existing worktrees and usage-probe dialog handling are outside this change.
