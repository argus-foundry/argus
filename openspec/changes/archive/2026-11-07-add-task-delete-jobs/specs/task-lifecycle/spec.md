## ADDED Requirements

### Requirement: Durable bulk task deletion

Bulk task deletion SHALL persist each task's worktree, branch and repository before stopping its session or deleting its record. A failed snapshot write or an unconfirmed session exit SHALL preserve the record and resources. A successful Stop acknowledgement SHALL be followed by a bounded wait for observed process exit. Supervisor confirmation SHALL use authoritative session and pending-restart RPCs; output stream loss alone SHALL NOT confirm exit. A confirmed absent session MAY proceed. When `allow_dirty` is false and the worktree exists, it SHALL check dirtiness at admission and again after confirmed session exit, under the task guard and repository ownership fence, before deleting any log, artifact, record or Git resource. A missing worktree MAY proceed; any other worktree stat error at either check SHALL be skipped with `dirty_check_failed`. A dirty post-exit worktree SHALL be skipped with `dirty_worktree`; a failed post-exit dirty check SHALL be skipped with `dirty_check_failed`. Both SHALL preserve the record and all resources with `record_deleted:false`; the stopped session remains resumable, and a persisted skipped result SHALL remain skipped on job recovery. A Pending task SHALL be skipped with `task_creation_in_progress` only when a non-blocking creation ownership-fence probe reports contention. Settled failures SHALL remain deletable when that fence is available. Record deletion SHALL precede Git cleanup, and task success SHALL require independently verified cleanup results.

Cleanup SHALL fail closed on incomplete ownership reads. Creation and cleanup SHALL share a cross-process ownership file lock keyed by the canonical Git common directory, covering worktree creation through persistence and unwind, and cleanup ownership validation through removal. Linked source checkouts SHALL share the same fence and branch ownership identity. Waiting for the nested cleanup mutex SHALL honor cancellation and release the ownership fence promptly. Cleanup SHALL preserve canonical worktree identities and branches referenced by surviving tasks as either their branch or base branch. Survivor repository identity SHALL be resolved only for candidate branch or base-branch owners; canonical worktree ownership checks SHALL still cover all survivors. For existing records, lock setup and relevant ownership identity checks SHALL run before record deletion and be revalidated before cleanup. Managed worktrees SHALL be contained in canonical managed roots and registered with the expected repository before removal. Git subprocesses SHALL have bounded lifetimes and their original process groups SHALL be terminated on every return path, including output-drain timeouts. `exec.ErrWaitDelay` SHALL produce a failed cleanup outcome even if Git already changed refs. API shutdown SHALL cancel delete jobs, stop between tasks, and drain within a bounded deadline before releasing its dependencies. Unfinished checkpoints SHALL remain resumable.

#### Scenario: Snapshot write fails

- **WHEN** the pre-deletion snapshot cannot be persisted
- **THEN** no session, record, log, artifact or Git resource is deleted and the failure is retained when storage permits

#### Scenario: Restart in the record checkpoint window

- **WHEN** the record is gone but its deletion flag was not persisted
- **THEN** the saved snapshot allows cleanup to resume without repeating record deletion

#### Scenario: Creation overlaps stale cleanup

- **WHEN** a fresh task has created its worktree but has not yet persisted its row
- **THEN** stale cleanup waits for creation and preserves the new owner's worktree and branches

#### Scenario: Surviving stacked child

- **WHEN** a surviving task references the selected task's branch as its base
- **THEN** local and remote branch cleanup is skipped with an ownership protection reason

#### Scenario: TUI creation overlaps daemon cleanup

- **WHEN** the TUI process pauses creation after creating a worktree and before persisting its row
- **THEN** daemon cleanup cannot acquire ownership until creation releases its file lock, and the new worktree and refs survive

#### Scenario: Creation is persisted before startup

- **WHEN** a selected task is pending and its initial session startup has not been committed
- **THEN** bulk deletion skips it with `task_creation_in_progress`, and creation can complete with its record and worktree intact

#### Scenario: Stop acknowledgement precedes exit

- **WHEN** Stop returns successfully while the session still runs
- **THEN** deletion waits for observed exit, and preserves the record, logs, artifacts and Git resources if exit cannot be confirmed within the deadline

#### Scenario: API startup races deletion

- **WHEN** `/resume` or `/restart` and bulk deletion target the same task in one API server
- **THEN** one per-task guard serializes them from authoritative lookup through startup or destructive cleanup

#### Scenario: Session appears before Git cleanup

- **WHEN** the final session-presence check finds a live session, visible startup reservation or queued restart after record deletion
- **THEN** the task result is failed with `session_appeared` and its worktree and branches are preserved; an unavailable authoritative check also fails closed

#### Scenario: Settled startup failure

- **WHEN** a task is Pending after a completed failed restart or returned ambiguous-start attempt and its creation ownership fence is free
- **THEN** the task remains eligible for bulk deletion
