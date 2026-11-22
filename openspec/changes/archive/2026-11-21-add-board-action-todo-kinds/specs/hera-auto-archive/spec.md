## MODIFIED Requirements

### Requirement: Done-done predicate

The system SHALL define a role as archivable as MERGED only when ALL of the following hold, and SHALL refuse with the first failing reason otherwise: its task status is `complete`; the role is not `blocked` (a live session parked at a prompt additionally refuses at archive time; the daemon's sticky needs-input set is not consulted); the role has no sent mail its recipient has not yet read (unread mail TO the role does not block: it is forwarded on archive); the task is bound to no other live role (a sub-coordinator's own coordinator hat in a bridged child orchestrator excepted); no plan-DAG node depends on it that is not itself done or archived; if it is a sub-coordinator (linked via `hera_orch_links` OR sharing its task with a child orchestrator's coordinator hat, the plan-DAG `kind=subcoord` shape), every child role is archived and it owns no open `decision`, `action` or `todo` board item (open `fyi` items are re-posted to the parent coordinator's board and resolved, not blocking); its worktree has no dirty files and its branch is confirmed landed by the merge-safety classifier (or it has neither worktree nor branch); and it is not pinned, resident, or a standing-team member. Any unconfirmed or errored check SHALL refuse.

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

- **WHEN** a sub-coordinator with two open `fyi` board items and no open `decision`, `action` or `todo` items is archived
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

#### Scenario: Open obligations prevent archive

- **WHEN** a sub-coordinator owns an open action or board todo, even with a pending trigger
- **THEN** normal archive refuses naming the obligation so it cannot disappear with the coordinator


### Requirement: Archive is race-safe and atomic

`hera_archive` SHALL perform every DB-side re-verification and every write in ONE database transaction: the task is still in a status allowed for its archive kind; the role and task are not pinned, resident or standing-team; the role is not `blocked`; no other live role is bound to the task (a sub-coordinator's own coordinator hat excepted); no hera mail sent by the role to a live recipient is unread; no live plan-DAG dependent; every child role archived; no open `decision`, `action` or `todo` board item. In that same transaction it SHALL mark the role's unread mail read and forward its digest to the coordinator, mark stale daemon merge notices read, re-post the sub-coordinator's open `fyi` items to the parent coordinator's board, end the child coordinator binding(s), archive the child orchestrator(s) (both `hera_orch_links` children and plan-DAG `kind=subcoord` children), archive the task, hide the role, and write the archive record. Git safety (merged or handed-off) SHALL be checked before the transaction and re-checked immediately before it opens (a git subprocess must not run under the database lock), and session idleness SHALL be re-checked as the last step before commit. If any check or write fails, the whole transaction SHALL roll back, nothing SHALL be written, and the tool SHALL report the role as refused with the reason. Events SHALL be emitted only after commit.

#### Scenario: State changing after the predicate stops the archive

- **WHEN** a new live binding, unread outbox mail, a reopened status, a live dependent, a live child role, or an open decision/action/todo item appears after the predicate passed
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
