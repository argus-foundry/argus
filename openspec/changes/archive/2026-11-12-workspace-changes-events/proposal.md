# Live Changes snapshots

## Why
Adopt the merged git REST and SSE contracts in Theia.

## What changes
The workspace SHALL consume matching worktree.changed events directly even during task snapshot buffering, deduplicate opaque revisions, fence snapshots invalidated in flight, surface stale snapshots and retry them with bounded backoff, and converge on reconnect/resync. Hidden panes SHALL defer reads until shown. Changes SHALL have no discovery interval; Artifacts retains its existing interval.

## Non-Goals
Frontend only. No daemon, TUI, legacy SPA or macOS edits. Named follow-up `macos-workspace-changes-events` covers native adoption. Server line counts remain deferred under `git-per-file-numstat`.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| ToolRefresh | Policy | Visible snapshot invalidation and request ownership | worktree.changed, git_revision |
| Comparison | Value | Server comparison provenance | status and diff responses |
| File discard | Operation | Confirmed captured local mutation | shared confirmation, task lifetime |
| Host reveal | Operation | Master-only captured Finder action | credential lifetime, reveal response |

Existing ToolRefresh, TaskChanges, TaskReview and rowMenu retain their current boundaries. Mutation policies live beside their domain owner.
