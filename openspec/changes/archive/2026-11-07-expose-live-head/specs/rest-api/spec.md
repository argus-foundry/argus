## ADDED Requirements

### Requirement: Live HEAD in git status

`GET /api/tasks/{id}/git/status` SHALL include `head` as `{"branch":string|null,"oid":string|null,"detached":bool}` read from the worktree's current HEAD. Detached HEAD has `branch` null and `detached` true; an unborn branch has `oid` null. `Task.Branch` SHALL remain the recorded branch and SHALL NOT be rewritten or substituted. An unreadable HEAD SHALL return 500 `{"error","code":"git_head_unavailable"}`.

#### Scenario: Checkout
- **WHEN** a branch is checked out inside the worktree
- **THEN** `head.branch` is that branch, `git_revision` changes, and `task.branch` is unchanged

#### Scenario: Detached
- **WHEN** HEAD is detached
- **THEN** `head.branch` is null and `head.detached` is true
