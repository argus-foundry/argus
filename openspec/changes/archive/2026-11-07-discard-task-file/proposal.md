## Why

The web Changes view can inspect a file but cannot discard its edits.

## What Changes

- `POST /api/tasks/{id}/git/discard` with `{path, scope:"local", delete_untracked}` restores one path to HEAD (staged and unstaged).
- Untracked and never-committed staged files require `delete_untracked=true`; renames, unmerged paths and clean paths return 409.
- On success the shared `worktree.changed` event is published (reason `discard`, `paths`) through the same hub, after the status cache has refreshed.

## Non-Goals

Web consumer: ENG-1104. TUI consumer: ENG-1120. A native macOS discard action is a named parity follow-up. No batch or committed-history reset.
