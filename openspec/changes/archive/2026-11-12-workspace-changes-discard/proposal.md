# Confirmed local file discard

## Why
Adopt the merged git REST and SSE contracts in Theia.

## What changes
The workspace SHALL offer local file discard only for dirty regular file entries, capture task and path before confirmation, initially focus Cancel and explain restoration to HEAD or explicit untracked deletion. It SHALL send scope local and explicit delete_untracked, never retry automatically, retain failures and list state, explain documented error codes, and refresh status and any open diff through worktree.changed.

## Non-Goals
Frontend only. No daemon, TUI, legacy SPA or macOS edits. Named follow-up `macos-workspace-changes-discard` covers native adoption. Server line counts remain deferred under `git-per-file-numstat`.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| ToolRefresh | Policy | Visible snapshot invalidation and request ownership | worktree.changed, git_revision |
| Comparison | Value | Server comparison provenance | status and diff responses |
| File discard | Operation | Confirmed captured local mutation | shared confirmation, task lifetime |
| Host reveal | Operation | Master-only captured Finder action | credential lifetime, reveal response |

Existing ToolRefresh, TaskChanges, TaskReview and rowMenu retain their current boundaries. Mutation policies live beside their domain owner.
