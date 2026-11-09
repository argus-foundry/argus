- [x] `task_delete_jobs` table and store methods
- [x] `agent.CleanupTaskGit` with verification and per-repo lock
- [x] Handlers, job runner, restart resume, `task_delete_job.updated` event
- [x] Tests: validation, idempotency, partial failure, preconditions, coordinator, resume, path reuse, real-git cleanup
- [x] README Reference rows and gotchas

- [x] Enforce durable checkpoints, stop failures and strict complete request bodies.
- [x] Fence creation against strict canonical ownership validation and protect stacked bases.
- [x] Restrict checked removal to registered managed worktrees and bound Git subprocesses.
- [x] Quiesce jobs before dependency teardown and coalesce unchanged public progress.
- [x] Add crash recovery, interleaving, failure, containment and mutation regression coverage.
- [x] Archive the task lifecycle amendment and name the macOS adoption follow-up.

- [x] Share ownership file locks across TUI and daemon processes using Git common-directory identity.
- [x] Wait for confirmed process exit and reject uncommitted startup or pending restart before deletion.
- [x] Cancel cleanup-mutex waits and terminate dirty-check subprocess groups.
- [x] Add registered containment, reverse cleanup interleaving, linked-checkout and case-alias mutation regressions.

- [x] Serialize API resume/restart and bulk deletion per task and recheck session presence before Git cleanup.
- [x] Probe actual creation ownership, allow settled Pending failures, and validate only relevant owner identities before row removal.
- [x] Terminate Git subprocess groups on every return and fail output-drain timeouts.
- [x] Add replacement, actual exit bound, lock-directory failure and non-nil skip-code mutation guards.
- [x] Name the approved full lifecycle exclusion follow-up and document the narrow API boundary.
