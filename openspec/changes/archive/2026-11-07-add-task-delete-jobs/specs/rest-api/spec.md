## ADDED Requirements

### Requirement: Tracked bulk task delete

The system SHALL expose `POST /api/tasks/bulk-delete` accepting `task_ids` (1 to 100 unique non-blank ids), an optional `idempotency_key` and `preconditions` (`allow_dirty`, `require_pr_closed`), and SHALL return 202 `{"job_id","status"}`, answering 400 for a malformed, empty, oversized, duplicate or blank selection. The complete request SHALL be a single JSON document within 64 KiB; malformed or oversized bodies SHALL return 400 `invalid_body`. Repeating a key with the same body SHALL return the same job; a different body SHALL return 409 `idempotency_conflict`. `GET /api/task-delete-jobs/{job_id}` SHALL return the job status, `completed`, `total` and per-task results (404 for an unknown job). Jobs SHALL be persisted, SHALL continue after the request disconnects, and SHALL be resumed after a daemon restart without repeating unsafe side effects. Per task the system SHALL skip missing tasks, live Hera coordinators (`hera_cascade_required`), dirty worktrees unless `allow_dirty`, and, when `require_pr_closed`, tasks whose cached PR state is not merged-closed, leaving them intact. It SHALL skip a Pending task with `task_creation_in_progress` only while its creation ownership fence is busy. Settled restart failures and returned ambiguous-start attempts remain eligible for deletion. It SHALL otherwise stop the session and confirm exit within a bounded deadline. When `allow_dirty` is false and the worktree exists, it SHALL check dirtiness at admission and again after confirmed session exit, under the task guard and repository ownership fence, before deleting any log, artifact, record or Git resource. A missing worktree MAY proceed; any other worktree stat error at either check SHALL be skipped with `dirty_check_failed`. A dirty post-exit worktree SHALL be skipped with `dirty_worktree`; a failed post-exit dirty check SHALL be skipped with `dirty_check_failed`. Both SHALL preserve the record and all resources with `record_deleted:false`; the stopped session remains resumable, and a persisted skipped result SHALL remain skipped on job recovery. After these checks it SHALL delete the record (emitting `task.deleted`) and remove the worktree and local and remote branches under the per-repo cleanup lock, touching only argus-managed worktrees and `argus/*` branches, and SHALL report a task `succeeded` only after git confirms each removal. The system SHALL emit `task_delete_job.updated` with `{"job_id","status","completed","total"}` on each progress change.

#### Scenario: Partial cleanup failure

- **WHEN** the branch of one selected task cannot be deleted
- **THEN** that task is `failed` with `record_deleted:true` and an error, and the other tasks are unaffected

#### Scenario: Protected tasks

- **WHEN** a selected task is a live coordinator, dirty, or lacks a closed PR under `require_pr_closed`
- **THEN** its result is `skipped` with the matching code and the task still exists

#### Scenario: Restart after record deletion

- **WHEN** the daemon restarts after a record was deleted but before cleanup finished
- **THEN** only the git cleanup is repeated, and it leaves any worktree or branch now owned by another task alone

#### Scenario: Session writes during stop

- **WHEN** a selected task passes the admission dirty check but becomes dirty during stop, or its post-exit dirty check fails, with `allow_dirty:false`
- **THEN** deletion skips the task with `dirty_worktree` or `dirty_check_failed`, preserves its record, logs, artifacts, worktree and branches, and recovery retains that skipped result
