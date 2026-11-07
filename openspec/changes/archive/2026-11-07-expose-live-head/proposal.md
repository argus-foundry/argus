## Why
After a checkout inside a task worktree, clients still show the creation-time branch and the PR poller looks up that stored name, so users can inspect the wrong PR.

## What Changes
- `gitutil.ReadHead` resolves the live HEAD (`branch`, `oid`, `detached`); `FetchGitStatus` carries it as `head` in the cached status snapshot.
- `GET /api/tasks/{id}/git/status` returns `head`; an unreadable HEAD is 500 `git_head_unavailable`.
- The automatic PR poller queries the live HEAD branch; detached or unreadable HEAD is skipped.
- `Task.Branch` stays the recorded lifecycle owner: never rewritten, still used by cleanup and deletion.

## Non-Goals
- No web, macOS, or TUI UI changes. Follow-up: ENG-1114 (Web: Show and copy the current worktree branch).
- No new event: consumers refresh on `worktree.changed` (HEAD changes already move `git_revision`).
