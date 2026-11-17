## Why

Claude Code 2.1.326 asks for folder trust on every `--add-dir` directory. Argus launches every Claude session with `--add-dir ~/.argus/skills` to load its builtin skills, so every new agent now stops at an "Adding a new directory" trust dialog before it can work.

## What Changes

- When Argus seeds Claude trust for a new worktree, it also seeds trust for the Argus-managed builtin skills workspace (`~/.argus/skills`) in the same account config.
- Same off switch (`[agents] trust_worktrees = false`), same lock and fail-open behavior as worktree trust.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `claude-worktree-trust`: also trust the builtin skills directory Argus passes with `--add-dir`.

## Impact

- `internal/agent/claudetrust.go` (the Claude trust hook), reusing the existing skills materialization seam.
- No wire, REST or client change; TUI, web and macOS creation share the daemon path.
- Not covered: the resident agent's extra user-skills `--add-dir` root, and tasks moved to another account after creation (same limit worktree trust already has).
