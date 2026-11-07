## Why
A stacked task's Changes list and file diff include its parent tasks' commits, because the git helpers pick the merge-base from upstream/default branches and ignore `Task.BaseBranch`.

## What Changes
- `gitutil.FetchGitStatus` and `FetchFileDiff` take an `Options{BaseBranch}` struct (extensible) and resolve the requested base before the upstream/default fallback.
- Both results carry `comparison` (`requested_base`, `resolved_base`, `merge_base`, `fallback_reason`).
- `GET /api/tasks/{id}/git/status` and `/git/diff` pass the task's base and return 500 on git collection failure.
- The per-file diff no longer fabricates an "added" diff for tracked, unchanged files.
- TUI call sites pass the task's BaseBranch.

## Non-Goals
- No web, macOS, or TUI UI changes. Follow-up: ENG-1106 (Web: Explain the Changes comparison base).
- No new event. Per-file line counts and worktree.changed are separate tickets.
