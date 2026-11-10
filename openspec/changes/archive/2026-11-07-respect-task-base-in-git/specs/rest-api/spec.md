## MODIFIED Requirements

### Requirement: Git status and diff are path-traversal safe

The server SHALL return git status, file diff, and file-tree listings scoped to a task's worktree. These SHALL return 404 when the task is unknown or has no assigned worktree. Git status and diff SHALL return 500 when the assigned worktree is missing or not a usable repository; an unborn HEAD alone SHALL NOT be a collection error. Diff and file-tree path parameters SHALL reject absolute paths and any path containing `..` with 400, so the diff cannot be coerced into reading files outside the worktree.

#### Scenario: Diff rejects absolute or dotdot paths

- **WHEN** a git-diff request supplies an absolute path or a path containing `..`
- **THEN** the server responds 400 Bad Request

#### Scenario: Git status for an unknown worktree

- **WHEN** a git-status request targets a task with no worktree
- **THEN** the server responds 404 Not Found

#### Scenario: Assigned worktree is not a usable repository

- **WHEN** status or a file diff is requested for an assigned worktree that is missing or not a repository
- **THEN** the server responds 500 rather than returning an empty or synthetic success

## ADDED Requirements

### Requirement: Base-aware git status and diff

`GET /api/tasks/{id}/git/status` and `GET /api/tasks/{id}/git/diff?path=` SHALL compare against the task's `base_branch` when set and include a `comparison` object in the response. Existing fields keep their shapes. A git collection failure SHALL return 500 rather than an empty result.

#### Scenario: Comparison metadata
- **WHEN** a stacked task's status is requested
- **THEN** the response includes `comparison.requested_base` equal to the task's base

#### Scenario: Git failure
- **WHEN** the worktree is not a git repository
- **THEN** the response is 500
