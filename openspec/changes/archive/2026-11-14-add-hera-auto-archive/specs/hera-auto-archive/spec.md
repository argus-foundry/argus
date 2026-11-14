## ADDED Requirements

### Requirement: Done-done predicate

The system SHALL define a role as archivable only when ALL of the following hold, and SHALL refuse with the first failing reason otherwise: its task status is `complete`; the role is not `blocked` (a live session parked at a prompt additionally refuses at archive time; the daemon's sticky needs-input set is not consulted); the role has no unread hera mail to it and no sent mail its recipient has not yet read; the task is bound to no other live role (a sub-coordinator's own coordinator hat in a bridged child orchestrator excepted); no plan-DAG node depends on it that is not itself done or archived; if it is a sub-coordinator (linked via `hera_orch_links` OR sharing its task with a child orchestrator's coordinator hat, the plan-DAG `kind=subcoord` shape), every child role is archived and it owns no open `decision` board item (open `fyi` items are re-posted to the parent coordinator's board and resolved, not blocking); its worktree has no dirty files and its branch is confirmed landed by the merge-safety classifier (or it has neither worktree nor branch); and it is not pinned, resident, or a standing-team member. Any unconfirmed or errored check SHALL refuse.

#### Scenario: Completed role with a merged PR is archivable

- **WHEN** a role's task is `complete`, has no unread mail, no dependents, and the classifier reports its branch Safe
- **THEN** the predicate returns archivable

#### Scenario: Open PR refuses

- **WHEN** a complete role's branch is not confirmed landed (open PR or unmerged commits)
- **THEN** the predicate refuses with a reason naming the unlanded branch

#### Scenario: Classifier failure refuses

- **WHEN** the merge-safety classifier returns an error or unknown verdict
- **THEN** the predicate refuses rather than treating the role as safe

#### Scenario: Sub-coordinator fyi items are pushed to the parent

- **WHEN** a sub-coordinator with two open `fyi` board items and no open `decision` items is archived
- **THEN** both items appear on the parent coordinator's board and the originals are resolved

#### Scenario: Live dependent refuses

- **WHEN** a not-yet-done plan node is blocked by the role
- **THEN** the predicate refuses naming the dependent

### Requirement: hera_archive coordinator tool

The system SHALL provide a coordinator-only `hera_archive(cwd, [role_name], [orchestrator], [dry_run])` MCP tool with `hera_accept`'s authorization shape (caller holds a live coordinator binding in the target's orchestrator; target is not the caller's own role). With `role_name` it evaluates that role; without it, it sweeps every role the caller coordinates. For each archivable role it SHALL mark the task archived, hide the role, and stop an idle live session, releasing its processes (agent, browser, and any dev stack via the existing finished-task sweepers); a busy session SHALL cause refusal. The tool SHALL refuse a role bound to the caller's own task, and a sweep SHALL stop evaluating after a bounded time budget and report how many roles remain. It SHALL return the archived roles and each refused role with its reason. With `dry_run` it SHALL write nothing. The archive primitive SHALL be idempotent on an already-archived role (archived roles are no longer addressable by name through the tool, which reports them as not found).

#### Scenario: Single role archived

- **WHEN** a coordinator calls `hera_archive` for an archivable role
- **THEN** the task is archived, the role is hidden, and the result lists it as archived

#### Scenario: Sweep reports refusals

- **WHEN** a coordinator calls `hera_archive` without `role_name` and one of three roles has an open PR
- **THEN** two roles are archived and the third is listed with its refusal reason

#### Scenario: Dry run writes nothing

- **WHEN** a coordinator calls `hera_archive` with `dry_run` true
- **THEN** verdicts are returned and no task or role changes

#### Scenario: Non-coordinator rejected

- **WHEN** a worker calls `hera_archive`
- **THEN** the tool returns an error stating only coordinators may archive

#### Scenario: Top-level coordinator not self-archivable

- **WHEN** a coordinator targets its own role, or a top-level coordinator with no parent
- **THEN** the tool refuses explaining it must be archived by the human or parent coordinator

### Requirement: Archive is non-destructive

Archiving via `hera_archive` SHALL NOT remove the worktree or delete any local or remote branch, and SHALL NOT kill a busy session. The role and task remain restorable by unarchive.

#### Scenario: Worktree preserved

- **WHEN** a role with a worktree whose branch merged is archived by `hera_archive`
- **THEN** the worktree directory and branch still exist afterward

### Requirement: Archive is race-safe and atomic

`hera_archive` SHALL perform every DB-side re-verification and every write in ONE database transaction: the task is still `complete`; the role and task are not pinned, resident or standing-team; the role is not `blocked`; no other live role is bound to the task (a sub-coordinator's own coordinator hat excepted); no unread hera mail to the role or sent by it to a live recipient; no live plan-DAG dependent; every child role archived; no open `decision` board item. In that same transaction it SHALL re-post the sub-coordinator's open `fyi` items to the parent coordinator's board, end the child coordinator binding(s), archive the child orchestrator(s) (both `hera_orch_links` children and plan-DAG `kind=subcoord` children), archive the task, and hide the role. Git safety SHALL be checked before the transaction and re-checked immediately before it opens (a git subprocess must not run under the database lock), and session idleness SHALL be re-checked as the last step before commit. If any check or write fails, the whole transaction SHALL roll back, nothing SHALL be written, and the tool SHALL report the role as refused with the reason. Events SHALL be emitted only after commit.

#### Scenario: State changing after the predicate stops the archive

- **WHEN** a new live binding, unread mail, a reopened status, a live dependent, a live child role, or an open decision item appears after the predicate passed
- **THEN** the transaction rolls back and nothing is archived

#### Scenario: Any failing write step rolls everything back

- **WHEN** the fyi push-up, the child coordinator binding end, the child orchestrator archive, the task archive, or the role hide fails
- **THEN** none of the other writes persist and the role is reported refused

#### Scenario: Sub-coordinator archive archives its child orchestrator

- **WHEN** a done-done sub-coordinator is archived
- **THEN** its child orchestrator is archived, its child coordinator binding is ended, and its open fyi items are on the parent board, all in one commit
