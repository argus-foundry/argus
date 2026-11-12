# Host Finder reveal

## Why
Adopt the merged git REST and SSE contracts in Theia.

## What changes
The workspace SHALL offer worktree and file Finder actions explaining before activation that Finder opens on the daemon host. It SHALL POST {} or the exact relative path, disable actions after master-only 403 for the credential lifetime, and distinguish unsupported host, missing path/task, invalid path and launch failure without automatic retries.

## Non-Goals
Frontend only. No daemon, TUI, legacy SPA or macOS edits. Named follow-up `macos-workspace-changes-reveal` covers native adoption. Server line counts remain deferred under `git-per-file-numstat`.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| ToolRefresh | Policy | Visible snapshot invalidation and request ownership | worktree.changed, git_revision |
| Comparison | Value | Server comparison provenance | status and diff responses |
| File discard | Operation | Confirmed captured local mutation | shared confirmation, task lifetime |
| Host reveal | Operation | Master-only captured Finder action | credential lifetime, reveal response |

Existing ToolRefresh, TaskChanges, TaskReview and rowMenu retain their current boundaries. Mutation policies live beside their domain owner.
