## Why

Aaron often cannot tell which hera roles and tasks are safe to clean up, so finished agents linger in the rail and task list. Coordinators already know when a role's work is truly done, but nothing lets them retire it safely. "Done-done" work should land in Archive automatically so the live views show only what still matters.

## What Changes

- Add a coordinator-only `hera_archive(cwd, [role_name], [orchestrator], [dry_run])` MCP tool. With `role_name` it archives one role; without it, it sweeps every role the caller coordinates. It archives only roles that pass a fail-closed "done-done" check and reports each refusal with its reason.
- Define done-done in one shared predicate (`internal/hera.ArchivableVerdict`): task `complete`, no pending needs-input/blocked flag, no unread mail either way, no live plan-DAG dependents, no live child roles (for coordinators), and branch work confirmed landed by the merge-safety classifier (or no worktree/branch at all). Anything unconfirmed is refused, never guessed safe.
- Archiving is non-destructive: it marks the task archived, hides the role, and stops an idle session. It never removes the worktree or branch.
- Update coordinator orientation and the `hera` skill so coordinators archive done-done roles proactively (after accept + confirmation, after a PR merges) and run a sweep before reporting "all done".
- A coordinator is done-done when every role under it is archived, its board is clear, and its own task is complete; the same predicate gates it.
- No daemon-side auto-archive in this change (named follow-up, see design).

## Capabilities

### New Capabilities
- `hera-auto-archive`: the done-done predicate, the `hera_archive` tool, and the non-destructive archive semantics.

### Modified Capabilities
- `hera-coordination`: coordinator orientation/skill guidance gains the proactive-archive duty; tool inventory gains `hera_archive`.

## Impact

- `internal/hera/` (new predicate + `ArchiveRole` primitive), `internal/mcp/hera.go` (tool), `internal/mergesafety` (consumed, unchanged), coordinator orientation text and builtin `hera` skill (re-record digest).
- REST/event surface unchanged: archived state already flows through existing `task.archived` events and role `archived_at`, so TUI, web, and macOS already render it. Frontend parity: no client change required; see design.
- README Reference MCP-tools table gains one row.
