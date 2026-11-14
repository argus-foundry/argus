## ADDED Requirements

### Requirement: Recovery candidates follow session liveness

With bounce recovery enabled, supervisor-mode reconciliation SHALL consider a dead task an orphan when it was recorded running before supervisor shutdown, is in_progress, or holds a live Hera binding with pending, in_progress or in_review task status. It SHALL exclude residents, archived/complete tasks and ready_to_close tasks. It SHALL leave supervisor-confirmed live sessions untouched and SHALL skip reconciliation when the supervisor live-set query fails. Snapshot evidence SHALL be consumed after successful reconciliation. Idle workers SHALL remain dormant even when their coordinator is also recovering. The legacy in-process path and disabled-recovery status-only reconciliation SHALL remain unchanged.

#### Scenario: Stale status on a live agent

- **WHEN** the supervisor recorded a pending plain task and an in_review coordinator as running before shutdown
- **THEN** both dead sessions enter recovery regardless of those statuses

#### Scenario: Crash fallback

- **WHEN** a supervisor crash leaves no shutdown snapshot and a pending or in_review task holds a live Hera binding
- **THEN** the dead task is considered for recovery unless it is closing, complete, archived or resident

#### Scenario: Idle worker remains dormant

- **WHEN** an orphaned worker was idle between turns and its coordinator is recovering
- **THEN** it is recorded dormant and listed in the coordinator note without automatic revival

### Requirement: In-place restarts restore running status

Every successful non-resident in-place session restart SHALL persist in_progress, including self-service recycle and live account moves. The resident SHALL retain its separately managed lifecycle. Failed definitive launches SHALL NOT report a successful running transition. Explicitly fresh recycle remains allowed; automatic bounce recovery SHALL NOT use a fresh launch.

#### Scenario: Recycle stale status

- **WHEN** an in_review or pending task successfully recycles its session
- **THEN** its task status becomes in_progress

#### Scenario: Account move restarts live session

- **WHEN** an in_review task moves its live session to another account
- **THEN** its successful restart persists in_progress

### Requirement: Automatic recovery requires a conversation

Automatic recovery SHALL fail an entry instead of starting when its conversation identity is empty or the backend reports the conversation unavailable. This check SHALL run immediately before the recovery start, including when state at death is unknown or the transcript disappeared after recording. Missing conversation entries SHALL carry reason no_transcript and SHALL never replay the original prompt.

#### Scenario: Unknown state and empty identity

- **WHEN** a recovery entry is unknown and its task has no conversation identity
- **THEN** the entry becomes failed without calling the session start primitive

#### Scenario: Transcript disappears before retry

- **WHEN** a recorded Claude conversation is no longer available at recovery start
- **THEN** recovery fails without fresh-launching its original prompt

## MODIFIED Requirements

### Requirement: Recovery ledger of bounce-killed sessions

When the supervisor-restart reconcile finds true orphans, the daemon SHALL record each non-resident, non-archived, non-complete orphan that is not ready_to_close in a durable ledger (task_meta namespace `bounce`) before any resume is attempted. Orphan selection SHALL follow the recovery-candidates liveness requirement. Each entry SHALL carry the kill time, the agent's state at death (`parked_question`, `mid_turn`, `idle`, or `unknown`), the pending question text and options when parked, the hera role kind and orchestrator, and a recovery status (`pending`, `resumed`, `dormant`, `delegated`, `failed`, `expired`, `superseded`). The ledger SHALL survive a daemon restart so an interrupted recovery continues. The daemon SHALL also record, for each recorded coordinator, eligible members of its team that were bound and not alive but not otherwise selected as `dormant` entries, so the coordinator is told about them and they are never auto-resumed. Each entry SHALL persist whether its resume note was delivered. Finished entries older than `expiry_hours` SHALL be deleted. When the ledger cannot be written, the daemon SHALL log the failure and continue serving.

#### Scenario: Orphans recorded before resume

- **WHEN** a supervisor restart leaves live-at-shutdown tasks with stale pending or in_review statuses and no live session
- **THEN** each eligible orphan gets a ledger entry with its kill time, state and role kind before the first resume starts

#### Scenario: Ledger survives daemon bounce

- **WHEN** the daemon restarts while entries are still `pending`
- **THEN** the recovery worker continues from the stored entries without re-classifying already-resumed ones

### Requirement: Resume policy

The recovery worker SHALL resume: every orphaned coordinator regardless of state; and every orphaned worker, freelance or plain task that has no recovering coordinator above it and whose state is `parked_question`, `mid_turn` or `unknown`. It SHALL NOT resume a worker or freelance whose coordinator is itself being resumed (status `delegated`: the coordinator revives it with `hera_revive`). Idle workers SHALL remain `dormant`, including under a recovering coordinator, and SHALL be listed in its note. A sub-coordinator SHALL be resumed directly even when its parent coordinator is also resumed: it owns its own team, and a parent's `hera_revive` of an already-running session is a safe no-op. A task without a resumable conversation SHALL NOT be resumed (a fresh launch would re-run its original prompt): its entry is `failed` with reason `no_transcript`. It SHALL leave idle-between-turns agents with no coordinator dormant. It SHALL NOT resume residents, archived tasks, complete tasks or ready_to_close tasks. Plain stopped tasks without prior-liveness evidence are not orphans. If a delegating coordinator cannot be resumed (failed, expired, superseded), its killed workers SHALL be re-evaluated as unsupervised. If the user archived or completed the coordinator, its workers SHALL be `superseded` with reason `coordinator_closed` and never resumed.

#### Scenario: Worker under a resumed coordinator

- **WHEN** a busy worker and its coordinator were both killed and the coordinator is resumable
- **THEN** the worker is `delegated` and the daemon does not start it

#### Scenario: Idle plain task

- **WHEN** a plain non-hera task was idle between turns at the bounce
- **THEN** it stays dormant and is not resumed

#### Scenario: Coordinator cannot be resumed

- **WHEN** the coordinator's task is archived or has no resumable session
- **THEN** its killed mid-turn workers are resumed directly unless that coordinator was explicitly archived or completed, which supersedes them

### Requirement: Recovery never overrides the user or a live session

The recovery worker SHALL skip an entry whose task now has a live session, is archived, is complete, is ready_to_close, or was resumed by someone else since the bounce, and SHALL log the skip. It SHALL never kick, stop or signal a live session. When a prior recovery start timed out but its eventual successful start receipt identifies the current live session, the worker SHALL mark that entry resumed and deliver its owed restart note under the same stable delivery ID without starting another session. A different launch receipt or PID SHALL NOT qualify for this exception.

#### Scenario: Human resumed first

- **WHEN** Aaron presses Enter on a dead pane before the worker reaches its entry
- **THEN** the worker skips it as superseded

#### Scenario: Timed-out start succeeded

- **WHEN** recovery's first start times out and its successful late RPC receipt matches the live session on retry
- **THEN** recovery delivers the restart note once without launching or stopping a session

#### Scenario: Unrelated live session

- **WHEN** another start replaced recovery's launch before retry
- **THEN** the entry is superseded and no restart note is delivered to that session
