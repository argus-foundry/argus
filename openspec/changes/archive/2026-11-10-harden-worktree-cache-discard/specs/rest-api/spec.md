## MODIFIED Requirements

### Requirement: Base-aware git status and diff

`GET /api/tasks/{id}/git/status` and `GET /api/tasks/{id}/git/diff?path=` SHALL compare against the task's `base_branch` when set and include a `comparison` object in the response. Existing fields keep their shapes. A cold status collection failure or a diff collection failure SHALL return 500 rather than an empty result. A warm status collection failure SHALL serve HTTP 200 with the last cached snapshot marked `stale=true`.

#### Scenario: Comparison metadata
- **WHEN** a stacked task's status is requested
- **THEN** the response includes `comparison.requested_base` equal to the task's base

#### Scenario: Git failure
- **WHEN** a cold status request or diff request targets a worktree that is not a git repository
- **THEN** the response is 500
#### Scenario: Warm collection failure
- **WHEN** a status refresh fails after a successful cached snapshot exists
- **THEN** status returns 200 with that snapshot and `stale=true`


### Requirement: Revisioned git status

`GET /api/tasks/{id}/git/status` SHALL include `git_revision`, `stale` and `watch_degraded`. `watch_degraded=true` indicates watcher allocation failed or exceeded a resource budget and reconciliation uses max-age refreshes. Archived tasks SHALL be served uncached and unwatched. A cold read SHALL await one bounded refresh and return 503 `{"error","code":"git_status_unavailable"}` on timeout. Cold collection failures return 500; warm collection failures retain HTTP 200 with stale cached data. Existing 404 and authentication behavior is unchanged.

#### Scenario: Cached read
- **WHEN** status is requested twice with no change
- **THEN** both responses carry the same `git_revision` and `stale=false`

#### Scenario: Cold timeout
- **WHEN** the first refresh exceeds the bound
- **THEN** the response is 503 with code `git_status_unavailable`
#### Scenario: Slow watcher setup
- **WHEN** watcher traversal is slow
- **THEN** it runs off the request path and does not delay the bounded cold status response

#### Scenario: Resource fallback
- **WHEN** watcher allocation fails or a budget is exceeded
- **THEN** the whole partial watcher is closed and later status responses expose `watch_degraded=true`


### Requirement: Discard one task file

`POST /api/tasks/{id}/git/discard` SHALL accept `{path, scope, delete_untracked}` (scope `local` only) and return 200 `{path, discarded:true, scope}`. Invalid paths return 400 `invalid_path`; unknown task or worktree 404; untracked without the flag, renames, conflicts and clean paths 409; git failures 500, each as `{"error","code"}`. Archived tasks return 409 `task_archived`. Discard and archive SHALL share the per-task lock, and discard SHALL re-read task state under that lock. No event is emitted on failure.

#### Scenario: Untracked requires flag
- **WHEN** an untracked file is discarded without `delete_untracked`
- **THEN** the response is 409 `untracked_requires_delete` and the file remains
#### Scenario: Archive fences discard
- **WHEN** discard arrives after archive or waits behind an archive mutation
- **THEN** it returns 409 `task_archived` without changing file or index contents
