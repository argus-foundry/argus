## Context

Today, "done" and "archived" are independent axes. `hera_accept` marks a task `complete` and asks the role to confirm it is winding down. Archive is separate: `db.SetArchived` flips `tasks.archived`, drops queued messages, and wipes `task_meta`; `ArchiveHeraRole` hides the role (the rail `a` key also stops the session); `heraReclaimAndArchiveTask` (nuke) additionally destroys the worktree/branch and is human-driven. The merge-safety classifier (`internal/mergesafety`, Tiers A/B/C/D) already answers "did this branch land?" fail-closed, but only the Cleanup popup uses it. There is no coordinator-callable way to archive; the generic `task_archive` toggle has no safety check and does not touch the role.

## Goals / Non-goals

- Goal: coordinators retire done-done roles safely and automatically; Aaron's live views show only live work.
- Non-goal: destroying worktrees/branches (nuke/Cleanup keep that), daemon-side background archiving, changing `hera_accept`.

## Decisions

**D1. Done-done definition (all must hold; first failure is the reported reason).**
1. Task status is `complete` (set by accept, worker self-close, or PR-merge self-assess). Not merely `in_review`.
2. Role not `blocked`, task not in the needs-input set.
3. No unread hera mail to or from the role (unread = unhandled; hera mail itself persists through archive, only the generic `task_messages` rows are deleted by `SetArchived`).
4. No live plan-DAG dependents: no planned/materialized node whose blocker is this role and is not itself done/archived.
5. Coordinator-kind role: every child role/sub-orchestrator archived and no open `decision` board item it owns. Open `fyi` items do NOT block: archive re-posts each to the parent coordinator's board (subject prefixed with the sub-coordinator's name) and resolves the original, so nothing is dropped.
6. Git: no worktree and no branch is fine. Otherwise the merge-safety verdict must be Safe (any tier) AND the worktree has no dirty files. Open PR, unmerged commits, unknown, or classifier error all refuse.
7. Not pinned, not resident-kind, not a standing-team member.
Rationale: each clause maps to something archive would silently discard or strand. Fail closed everywhere; "unknown" refuses.

**D2. Who acts: coordinator-driven, daemon backstop deferred.** A new coordinator-only `hera_archive` tool shares `hera_accept`'s authorization shape (live coordinator binding in the target's orchestrator, target not the caller's own role). Guidance tells coordinators to call it after accept+confirmation, after a PR-merge notice, and as a sweep before declaring the orchestrator finished. A daemon sweeper was considered and deferred: it would archive without any agent judgment about "future purpose", and the roles' check-in reply protocol already gives coordinators that judgment. Open decision for Aaron: add the backstop later?

**D3. Archive semantics: non-destructive.** `ArchiveRole` = (a) re-verify predicate inside the action, (b) `SetArchived(task, true)`, (c) `ArchiveHeraRoleAs(role, coordinator)`, (d) stop an idle live session (never a busy one: busy means refuse with reason), which tears down the session's process tree (agent, playwright/Chrome pairs). Dev stacks (devbox/process-compose/mysqld) are stopped by the existing finished-task devstack sweeper, which treats archived tasks as finished; the spec requires that hand-off and a test that an archived task's stack is swept. Worktree and branch are untouched, so a mistaken archive loses nothing; unarchive restores the row. Reclaiming disk stays with Cleanup/nuke. Note `SetArchived` wipes `task_meta`; this is existing behavior and acceptable post-complete.

**D4. Sweep and dry run.** Omitting `role_name` sweeps all coordinated roles, returning archived list and refused-with-reason list. `dry_run` reports verdicts without writing, so a coordinator can ask "what could I clean up?".

**D5. Coordinators.** A sub-coordinator's task is done-done when its own task is complete and D1.5 holds (children archived, board clear). A top-level coordinator is archived by Aaron (or its parent), not by `hera_archive`, since no coordinator above it exists; the tool refuses with that explanation.

**D6. Frontend parity.** The change adds no REST field, endpoint, event, or status semantics; it reuses `task.archived` and role `archived_at`, which TUI, web, and macOS already render (hidden/dimmed, Archive section). No client work. Named gap: no surface shows *why* a role was refused; that is returned only to the coordinator. Follow-up (not in scope): show archivable/refusal state in Cleanup popup.

## Risks

- Classifier network failures cause refusals (safe direction); coordinators retry on the next sweep.
- Coordinator mislabels work done: mitigated by status `complete` requirement plus git check; archive is reversible.
