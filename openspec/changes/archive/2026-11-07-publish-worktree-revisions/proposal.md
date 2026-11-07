## Why

The web Changes panel polls git status and cannot learn about file edits except through session activity. The daemon should keep a per-worktree cached status with an opaque revision and publish `worktree.changed` events.

## What Changes

- Daemon-side watcher hub (`internal/api/worktreewatch.go`): one fsnotify watcher per validated task worktree plus its git dir (HEAD and index only), 150ms debounce, single-flight status refresh, revision = hash of the status message including the comparison.
- `GET /api/tasks/{id}/git/status` gains `git_revision` and `stale`; a cold read waits for one bounded refresh (8s) and returns 503 `git_status_unavailable` on timeout.
- New SSE/ring event `worktree.changed` with `{revision, reason: filesystem|git|discard, paths?}`.

## Non-Goals

Web consumer: ENG-1107. TUI consumer: ENG-1121. macOS event-driven refresh is a named parity follow-up. Watchers register lazily on the first status or discard request, not at task creation.
