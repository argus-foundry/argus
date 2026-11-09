## Why

Deleting several selected tasks needs one request per task and leaves no server-owned operation to revisit after the tab closes. A successful single-task DELETE also does not prove git cleanup succeeded: `RemoveWorktreeAndBranch` logs failures and returns nothing.

## What changes

- `POST /api/tasks/bulk-delete` accepts `{"task_ids":[...],"idempotency_key":"...","preconditions":{"allow_dirty":false,"require_pr_closed":false}}` and returns 202 `{"job_id","status"}`. 1 to 100 unique, non-blank ids; otherwise 400.
- The same key with the same body returns the same job; a changed body returns 409 `idempotency_conflict`. A blank key never deduplicates.
- `GET /api/task-delete-jobs/{job_id}` returns `{"job_id","status","completed","total","results":[...]}`; 404 for an unknown job. Job status: `queued`, `running`, `complete`. Per-task state: `pending`, `running`, `succeeded`, `skipped`, `failed`; `worktree` / `local_branch` / `remote_branch`: `pending`, `removed`, `absent`, `failed`, `not_applicable`.
- Jobs persist in a new `task_delete_jobs` table and run on a daemon-owned goroutine, so a closed request does not cancel them. `ListenAndServe` resumes unfinished jobs.
- Per task: reject live coordinators (`hera_cascade_required`), check dirty worktree unless `allow_dirty` (`dirty_worktree`), check cached PR state is merged-closed when `require_pr_closed` (`pr_not_closed`), then stop the session, delete the record (existing `task.deleted` event), then clean git. Missing tasks are skipped (`task_not_found`). The skip code is returned in `error`.
- Git cleanup is `agent.CleanupTaskGit`: per-repo lock, argus-managed worktree paths and `argus/*` branches only, each result re-verified with git. A task is `succeeded` only when verification passes; otherwise `failed` with `record_deleted:true`.
- New SSE event `task_delete_job.updated` with `{"job_id","status","completed","total"}`; GET stays authoritative.

## Capabilities

### Modified capabilities

- `rest-api`: adds the bulk delete job endpoints and event.
- `task-lifecycle`: adds durable, ordered bulk deletion and restart ownership protection.

## Non-Goals

- Web consumption. Follow-up: ENG-1113 (Web: Delete selected tasks through a tracked server job). TUI single-task delete is unchanged; Mac adoption is deferred to `add-mac-tracked-bulk-delete`.
- Hera cascade deletion: coordinators are rejected, never cascaded.
- Cancelling a running job.
- Full task-lifecycle exclusion is deferred to `serialize-full-task-lifecycle`, covering non-API starts (TUI `startSession`, daemon revive/recycle), account-move restarts and ambiguous supervisor Start completion. This change serializes only `/resume`, `/restart` and bulk deletion within the API server, with a final session-presence recheck before Git cleanup.

## Impact

- Code: `internal/api/bulkdelete.go`, `internal/agent/cleanup_checked.go`, `internal/db/delete_jobs.go`, `internal/db/schema.go`, `internal/model/event.go`.
