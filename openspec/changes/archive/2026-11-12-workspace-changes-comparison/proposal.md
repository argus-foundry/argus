# Changes comparison provenance

## Why
Adopt the merged git REST and SSE contracts in Theia.

## What changes
The workspace SHALL label status and diff using returned comparison requested_base, resolved_base, merge_base and fallback_reason. It SHALL explain local HEAD changes, branch merge-base changes and untracked contents without inferring a per-file comparison kind absent from the contract. Fallback warnings SHALL name the effective base.

## Non-Goals
Frontend only. No daemon, TUI, legacy SPA or macOS edits. Named follow-up `macos-workspace-changes-comparison` covers native adoption. Server line counts remain deferred under `git-per-file-numstat`.

## Abstraction inventory
| Name | Type | Responsibility | Depends on |
| --- | --- | --- | --- |
| ToolRefresh | Policy | Visible snapshot invalidation and request ownership | worktree.changed, git_revision |
| Comparison | Value | Server comparison provenance | status and diff responses |
| File discard | Operation | Confirmed captured local mutation | shared confirmation, task lifetime |
| Host reveal | Operation | Master-only captured Finder action | credential lifetime, reveal response |

Existing ToolRefresh, TaskChanges, TaskReview and rowMenu retain their current boundaries. Mutation policies live beside their domain owner.
