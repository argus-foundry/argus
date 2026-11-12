# Design

## Decisions
- Cache and watcher are keyed per task and replaced when the worktree or the task's comparison base changes, so a status is never served for a different base.
- A cached entry older than 15s is served with `stale=true` and refreshed in the background, covering base-ref movement that no worktree watcher can see. This is a deliberate deviation from "watch only task worktrees".
- Watching is non-recursive per directory (fsnotify); new directories are added on Create, `.git` and `node_modules` are skipped, and at most 4096 directories are watched.
- The first computation for an entry emits no event; only revision changes after a baseline do.
- Unregistration happens on `task.deleted`/`task.archived` emission and via a 30s sweep that drops entries whose task or worktree is gone.
- Collection error with a cached value serves the cache with `stale=true`; a cold error returns 500 as before.
