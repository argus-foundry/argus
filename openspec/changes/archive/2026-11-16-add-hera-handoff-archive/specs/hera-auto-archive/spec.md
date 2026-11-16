## MODIFIED Requirements

### Requirement: Done-done predicate

The system SHALL define a role as archivable as MERGED only when ALL of the following hold, and SHALL refuse with the first failing reason otherwise: its task status is `complete`; the role is not `blocked` (a live session parked at a prompt additionally refuses at archive time; the daemon's sticky needs-input set is not consulted); the role has no sent mail its recipient has not yet read (unread mail TO the role does not block: it is forwarded on archive); the task is bound to no other live role (a sub-coordinator's own coordinator hat in a bridged child orchestrator excepted); no plan-DAG node depends on it that is not itself done or archived; if it is a sub-coordinator (linked via `hera_orch_links` OR sharing its task with a child orchestrator's coordinator hat, the plan-DAG `kind=subcoord` shape), every child role is archived and it owns no open `decision` board item (open `fyi` items are re-posted to the parent coordinator's board and resolved, not blocking); its worktree has no dirty files and its branch is confirmed landed by the merge-safety classifier (or it has neither worktree nor branch); and it is not pinned, resident, or a standing-team member. Any unconfirmed or errored check SHALL refuse.

#### Scenario: Completed role with a merged PR is archivable

- **WHEN** a role's task is `complete`, has no unread outbox mail, no dependents, and the classifier reports its branch Safe
- **THEN** the predicate returns archivable

#### Scenario: Open PR refuses the merged predicate

- **WHEN** a complete role's branch is not confirmed landed (open PR or unmerged commits)
- **THEN** the merged predicate refuses with a reason naming the unlanded branch

#### Scenario: Classifier failure refuses

- **WHEN** the merge-safety classifier returns an error or unknown verdict
- **THEN** the predicate refuses rather than treating the role as safe

#### Scenario: Sub-coordinator fyi items are pushed to the parent

- **WHEN** a sub-coordinator with two open `fyi` board items and no open `decision` items is archived
- **THEN** both items appear on the parent coordinator's board and the originals are resolved

#### Scenario: Live dependent refuses

- **WHEN** a not-yet-done plan node is blocked by the role
- **THEN** the predicate refuses naming the dependent

#### Scenario: Unread inbox mail does not refuse

- **WHEN** a complete, landed role has unread hera mail addressed to it
- **THEN** the predicate does not refuse on that mail

#### Scenario: Unread outbox mail still refuses

- **WHEN** a role has sent a message its live recipient has not read
- **THEN** the predicate refuses without `force`

### Requirement: hera_archive coordinator tool

The system SHALL provide a coordinator-only `hera_archive(cwd, [role_name], [orchestrator], [dry_run], [force], [reason])` MCP tool with `hera_accept`'s authorization shape (caller holds a live coordinator binding in the target's orchestrator; target is not the caller's own role). With `role_name` it evaluates that role; without it, it sweeps every role the caller coordinates. For each role passing the done-done (merged) predicate or the handed-off predicate it SHALL mark the task archived, hide the role, write an archive record, and stop a live session, releasing its processes (agent, browser, and any dev stack via the existing finished-task sweepers); a busy session SHALL cause refusal. The tool SHALL refuse a role bound to the caller's own task, and a sweep SHALL stop evaluating after a bounded time budget and report how many roles remain. It SHALL return the archived roles (each labelled `merged`, `handed off`, or `forced`) and each refused role with its reason. With `dry_run` it SHALL write nothing. `force` and `reason` are specified by the forced-archive requirement. The archive primitive SHALL be idempotent on an already-archived role (archived roles are no longer addressable by name through the tool, which reports them as not found).

#### Scenario: Single role archived

- **WHEN** a coordinator calls `hera_archive` for an archivable role
- **THEN** the task is archived, the role is hidden, an archive record exists, and the result lists it as archived with its kind

#### Scenario: Sweep reports refusals

- **WHEN** a coordinator calls `hera_archive` without `role_name` and one of three roles has a failing PR
- **THEN** two roles are archived and the third is listed with its refusal reason

#### Scenario: Dry run writes nothing

- **WHEN** a coordinator calls `hera_archive` with `dry_run` true
- **THEN** verdicts (including which kind would be recorded) are returned and no task, role, or record changes

#### Scenario: Non-coordinator rejected

- **WHEN** a worker calls `hera_archive`
- **THEN** the tool returns an error stating only coordinators may archive

#### Scenario: Top-level coordinator not self-archivable

- **WHEN** a coordinator targets its own role, or a top-level coordinator with no parent
- **THEN** the tool refuses explaining it must be archived by the human or parent coordinator

### Requirement: Archive is race-safe and atomic

`hera_archive` SHALL perform every DB-side re-verification and every write in ONE database transaction: the task is still in a status allowed for its archive kind; the role and task are not pinned, resident or standing-team; the role is not `blocked`; no other live role is bound to the task (a sub-coordinator's own coordinator hat excepted); no hera mail sent by the role to a live recipient is unread; no live plan-DAG dependent; every child role archived; no open `decision` board item. In that same transaction it SHALL mark the role's unread mail read and forward its digest to the coordinator, mark stale daemon merge notices read, re-post the sub-coordinator's open `fyi` items to the parent coordinator's board, end the child coordinator binding(s), archive the child orchestrator(s) (both `hera_orch_links` children and plan-DAG `kind=subcoord` children), archive the task, hide the role, and write the archive record. Git safety (merged or handed-off) SHALL be checked before the transaction and re-checked immediately before it opens (a git subprocess must not run under the database lock), and session idleness SHALL be re-checked as the last step before commit. If any check or write fails, the whole transaction SHALL roll back, nothing SHALL be written, and the tool SHALL report the role as refused with the reason. Events SHALL be emitted only after commit.

#### Scenario: State changing after the predicate stops the archive

- **WHEN** a new live binding, unread outbox mail, a reopened status, a live dependent, a live child role, or an open decision item appears after the predicate passed
- **THEN** the transaction rolls back and nothing is archived

#### Scenario: Any failing write step rolls everything back

- **WHEN** the mail forward, the fyi push-up, the child coordinator binding end, the child orchestrator archive, the task archive, the role hide, or the record write fails
- **THEN** none of the other writes persist (including the mail still being unread) and the role is reported refused

#### Scenario: Sub-coordinator archive archives its child orchestrator

- **WHEN** a done-done sub-coordinator is archived
- **THEN** its child orchestrator is archived, its child coordinator binding is ended, and its open fyi items are on the parent board, all in one commit

#### Scenario: Stale merge prompts do not block archive

- **WHEN** a complete role has only unread daemon merge prompts identified by durable provenance
- **THEN** dry run reports archivable without writing and archive marks the prompts read atomically with hiding the role

#### Scenario: Notice retirement rolls back

- **WHEN** any later archive check or write fails after stale prompts were marked read
- **THEN** the prompts remain unread and all archive writes roll back

## ADDED Requirements

### Requirement: Handed-off archive

`hera_archive` SHALL archive a worker or freelance role without waiting for its PR to merge when ALL of the following hold, otherwise refusing with the first failing reason: its PR is open (a draft counts) and its CI rollup is not failing (pending or absent checks pass); its branch is pushed with the local tip equal to the remote branch tip and to the PR head; its worktree has no uncommitted changes; its task status is `in_review` or `complete`; its session is stopped or idle (it is stopped as part of the archive); and every non-git done-done clause holds (not pinned, resident or standing-team; not blocked; no other live binding; no live plan dependents; sub-coordinator children archived with no open decision item; outbox clear). Any lookup error, missing PR, timeout, or ambiguous output SHALL refuse. The archive SHALL be recorded with kind `handed_off`, distinct from `merged`, in a durable record that survives the task archive.

#### Scenario: Open green PR with pushed clean branch is archived as handed off

- **WHEN** a worker's task is `in_review`, its PR is open with passing checks, local tip equals remote tip, and the worktree is clean
- **THEN** `hera_archive` archives the task and role and records kind `handed_off`

#### Scenario: Failing CI refuses

- **WHEN** the PR's check rollup is `FAILURE` or `ERROR`
- **THEN** the role is refused naming the failing checks

#### Scenario: Unpushed commits refuse

- **WHEN** the local branch tip differs from the remote branch tip
- **THEN** the role is refused naming the divergence

#### Scenario: Dirty worktree refuses

- **WHEN** the worktree has uncommitted changes
- **THEN** the role is refused

#### Scenario: Running task refuses

- **WHEN** the task status is `in_progress`
- **THEN** the role is refused

#### Scenario: Lookup failure fails closed

- **WHEN** the PR lookup, `git ls-remote`, or the status check errors or times out
- **THEN** the role is refused rather than archived

#### Scenario: Merged work is still recorded as merged

- **WHEN** a role passes the merged done-done predicate
- **THEN** it is recorded `merged`, not `handed_off`

### Requirement: Handed-off archives are watched until the PR settles

The daemon PR poller SHALL treat an archived task with an unsettled `handed_off` record as poll-eligible. When its PR merges, the system SHALL settle the record, advance a still-`in_review` task to `complete`, and post a `fyi` item on the coordinator's board. When its PR closes WITHOUT merging, the system SHALL, in one transaction, settle the record as closed-unmerged, un-archive the task, un-hide the role, roll the task to `in_progress` through the exact-status-gated revive, and post a `decision` board item on the coordinator's board naming the role and PR; a revive failure SHALL NOT suppress the board item. An open PR SHALL change nothing. Merge notices for archived roles SHALL NOT be sent through the role-mail channel. A coordinator-only `hera_unarchive(cwd, role_name, [orchestrator])` tool SHALL un-archive and revive a role the same way (record settled as handed back) without git checks.

#### Scenario: Merge settles the record

- **WHEN** the PR of a handed-off archived role merges
- **THEN** the record is settled, an `in_review` task becomes `complete`, and the coordinator board gets a `fyi` item

#### Scenario: Close without merge revives the role

- **WHEN** the PR of a handed-off archived role closes unmerged
- **THEN** the task is un-archived, the role is back on the rail in `in_progress`, and the coordinator board has an open `decision` item

#### Scenario: Revive failure still reaches the coordinator

- **WHEN** the revive step fails after a closed-unmerged PR
- **THEN** the board item is still posted and the failure is logged

#### Scenario: Settled and forced records are not polled

- **WHEN** a record is settled, or has kind `merged` or `forced`
- **THEN** the archived task is not poll-eligible

#### Scenario: Coordinator hands a role back

- **WHEN** a coordinator calls `hera_unarchive` for a handed-off archived role
- **THEN** the role is un-archived and revived and the record is settled as handed back

### Requirement: Forced archive

`hera_archive` SHALL accept `force=true` with a required non-empty `reason` (at most 1 KiB) for a single named role; `force` without `reason`, `reason` without `force`, and `force` without `role_name` SHALL be rejected. Forced archive SHALL skip the done-done, handed-off, unread-mail, status, dependents, child-role, git, and session-busy checks, SHALL stop a live session after commit, and SHALL store the reason in a `forced` archive record shown in role history and the inbox viewer and included in the forwarded mail digest. It SHALL remain coordinator-only. The following SHALL stay refusals under force, in both the preflight and the transaction: a coordinator role, a pinned role or task, a resident task, a standing-team member, an already archived/nuked/cancelled role, and a task also bound to another live role.

#### Scenario: Forced archive overrides an unmerged, failing PR

- **WHEN** a coordinator calls `hera_archive(role_name, force=true, reason="batch owns it")` on a role with a failing PR
- **THEN** the role and task are archived with a `forced` record carrying the reason

#### Scenario: Reason is mandatory

- **WHEN** `force=true` is passed with an empty or whitespace reason
- **THEN** the tool returns an error and writes nothing

#### Scenario: Hard refusals survive force

- **WHEN** `force=true` targets a pinned role, a resident task, a standing-team member, or a coordinator
- **THEN** the role is refused

#### Scenario: Force never sweeps

- **WHEN** `force=true` is passed without `role_name`
- **THEN** the tool returns an error

### Requirement: Unread mail is forwarded, not blocking

On any archive the system SHALL, in the archive transaction, mark the role's unread hera mail to it as read and insert ONE digest message from the archived role to the orchestrator's first live coordinator with one line per message (message id, sender role name, first line truncated to 200 bytes) and, for a forced archive, the reason. If the digest cannot be stored (coordinator inbox full, rate limit, no live coordinator), the archive SHALL roll back and refuse so that no mail is marked read without its summary delivered. Full message bodies SHALL remain retrievable through `hera_get_messages`.

#### Scenario: Unread mail is summarized to the coordinator

- **WHEN** a role with three unread messages is archived
- **THEN** the three messages are marked read and the coordinator's inbox gains one digest listing sender and first line of each

#### Scenario: Digest failure refuses the archive

- **WHEN** the coordinator's inbox is full
- **THEN** the archive is refused, nothing is marked read, and the reason names the full inbox

### Requirement: Argus archive and hera archive agree

Archiving a task through any argus surface (MCP `task_archive`, the REST archive endpoint used by web, macOS and remote TUI, and the local TUI) SHALL run the same hera verdict when the task has a live worker or freelance binding, and SHALL archive the role in the same transaction when the verdict passes, or refuse with the same reason and a message naming forced `hera_archive` when it does not. Tasks with no live worker or freelance binding, including a task bound only to a coordinator, SHALL archive as before (coordinators are archived from the Hera view and are outside this guard). A pending graduation SHALL keep its own refusal ahead of the hera verdict. The REST archive endpoint SHALL accept `{"force":true,"reason":"..."}` and SHALL return 409 with the refusal reason on refusal. As a backstop, `db.SetArchived(id, true)` and an archived 0-to-1 flip through `db.Update` SHALL refuse with `ErrHeraRoleLive` for a task with a live worker or freelance binding; the hera archive transaction and the nuke path (which ends the binding first) SHALL be unaffected. A hera archive SHALL archive the role's task in the same transaction.

#### Scenario: Passing verdict archives task and role together

- **WHEN** `task_archive` targets a hera-bound worker task that passes the verdict
- **THEN** the task is archived and the role is hidden in one step

#### Scenario: Refusal carries the hera reason

- **WHEN** `task_archive` targets a worker task whose PR is failing
- **THEN** the call is refused with the hera reason and a hint to use forced `hera_archive`

#### Scenario: Unguarded caller hits the DB backstop

- **WHEN** any code calls `db.SetArchived(id, true)` for a task with a live worker binding outside the hera path
- **THEN** it returns `ErrHeraRoleLive` and nothing is written

#### Scenario: Plain tasks are unaffected

- **WHEN** a task with no hera binding is archived from any surface
- **THEN** it archives exactly as before

#### Scenario: REST force

- **WHEN** the REST archive endpoint receives `{"force":true,"reason":"x"}` for a live-bound task that fails the verdict
- **THEN** the task and role archive with a `forced` record
