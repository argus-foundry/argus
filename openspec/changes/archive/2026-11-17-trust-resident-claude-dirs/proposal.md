## Why

The resident agent never goes through task creation, so Argus never seeds Claude trust for it. Its working directory and its two `--add-dir` skills directories are trusted only where someone once answered the dialog by hand. The resident also switches between Claude accounts on its own, so on any other account it stops at a folder trust dialog with nobody watching.

## What Changes

- Each resident Claude launch seeds trust, in the launched account's config, for the resident working directory, the builtin skills workspace and the resident user-skills `--add-dir` root.
- Same `[agents] trust_worktrees` opt-out, lock and fail-open rules as worktree trust; the launch continues on failure.
- The spawn surface version is bumped because the supervisor builds the resident command.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `claude-worktree-trust`: also trust the resident's launch directories on every resident launch.

## Impact

- `internal/agent/agent.go` (BuildCmd resident branch), `internal/agent/claudetrust.go`, `internal/daemon/surface.go` (spawn surface 53 and manifest).
- An older running supervisor keeps launching the resident without trust until it is restarted.
- No wire, REST or client change.
