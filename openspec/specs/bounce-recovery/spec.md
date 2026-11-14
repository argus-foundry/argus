# bounce-recovery Specification

## Purpose
After a supervisor restart kills agent sessions, the daemon records what each was doing, resumes the interrupted ones in throttled coordinator-first order, and tells each what happened with an [argus] note.
## Requirements
### Requirement: Recovery ledger of bounce-killed sessions

When the supervisor-restart reconcile finds true orphans, the daemon SHALL record each non-resident, non-archived, non-complete orphan that is not ready_to_close in a durable ledger (task_meta namespace `bounce`) before any resume is attempted. Orphan selection SHALL follow the recovery-candidates liveness requirement. Each entry SHALL carry the kill time, the agent's state at death (`parked_question`, `mid_turn`, `idle`, or `unknown`), the pending question text and options when parked, the hera role kind and orchestrator, and a recovery status (`pending`, `resumed`, `dormant`, `delegated`, `failed`, `expired`, `superseded`). The ledger SHALL survive a daemon restart so an interrupted recovery continues. The daemon SHALL also record, for each recorded coordinator, eligible members of its team that were bound and not alive but not otherwise selected as `dormant` entries, so the coordinator is told about them and they are never auto-resumed. Each entry SHALL persist whether its resume note was delivered. Finished entries older than `expiry_hours` SHALL be deleted. When the ledger cannot be written, the daemon SHALL log the failure and continue serving.

#### Scenario: Orphans recorded before resume

- **WHEN** a supervisor restart leaves live-at-shutdown tasks with stale pending or in_review statuses and no live session
- **THEN** each eligible orphan gets a ledger entry with its kill time, state and role kind before the first resume starts

#### Scenario: Ledger survives daemon bounce

- **WHEN** the daemon restarts while entries are still `pending`
- **THEN** the recovery worker continues from the stored entries without re-classifying already-resumed ones

### Requirement: State at death is read from the transcript

The daemon SHALL classify a Claude-family orphan from the tail of its session transcript, after the session ID refresh. An unanswered AskUserQuestion or ExitPlanMode tool call SHALL classify as `parked_question`. A last entry that is an assistant tool call, a tool result, a thinking block, a partial assistant message, or a user message with no assistant reply SHALL classify as `mid_turn`. A last assistant message that ended its turn, and a final `[Request interrupted by user` entry (the human pressed Esc, which also dismisses any open question), SHALL classify as `idle`. A missing or unreadable transcript, or a non-Claude backend, SHALL classify as `unknown`. Classification SHALL read a bounded tail and SHALL never block the reconcile.

#### Scenario: Parked on AskUserQuestion

- **WHEN** the transcript ends on an AskUserQuestion tool call with no tool result
- **THEN** the entry is `parked_question` and stores the question text and option labels

#### Scenario: Mid-turn

- **WHEN** the transcript ends on a tool result or an assistant tool call
- **THEN** the entry is `mid_turn`

#### Scenario: Unreadable transcript

- **WHEN** the transcript is missing or the backend is not Claude
- **THEN** the entry is `unknown` and is treated as `mid_turn` for resume policy, without question text

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

### Requirement: Throttled, coordinator-first resume

The recovery worker SHALL start resumes in order of orchestrator depth (shallowest coordinators first, then unsupervised agents), oldest kill first within a tier. It SHALL keep at most `max_concurrent` resumed sessions unsettled at once and SHALL wait at least `stagger_seconds` between starts. A resumed session counts as settled once it is idle at least 8 seconds after its start, or after a 60 second ceiling. The worker SHALL wait for the daemon's MCP port to be bound (unless the MCP server is disabled) before starting any resume, logging the wait once; expiry and pruning SHALL run before that gate. A failed start SHALL be retried with backoff up to 3 attempts, then marked `failed` and logged. Entries older than `expiry_hours` SHALL be marked `expired` and never resumed.

#### Scenario: Thirty orphans

- **WHEN** 30 agents are eligible
- **THEN** no more than `max_concurrent` are starting at any time and coordinators start before plain tasks

#### Scenario: Stale ledger

- **WHEN** the daemon was down longer than `expiry_hours`
- **THEN** entries are marked `expired` and nothing is resumed

### Requirement: Resume note delivered as an argus system turn

After a resumed session starts, the daemon SHALL deliver a note beginning with `[argus] ` through the idle-gated reliable-notify path so it is never mistaken for the user's message. The note SHALL state that a supervisor restart killed the session at the recorded time and what it was doing. For `parked_question` it SHALL quote the pending question and options as data and instruct the agent to re-ask it with AskUserQuestion. For a coordinator it SHALL list each killed worker and sub-coordinator with name, kind and state at death, and instruct it to resume them with `hera_revive` in waves of at most 3, busy ones first, and to post any open question to its board. Question text and option labels SHALL be quoted single-line data, never instructions. The note SHALL be delivered under a stable delivery ID; delivery state SHALL be persisted, an undelivered note SHALL be re-queued under the same ID after a daemon restart, and an entry SHALL count as noted only once delivery succeeded. The note SHALL never claim that none of a coordinator's workers were killed; with no recorded team it SHALL say none were recorded, that the record can be incomplete, and to check its roles.

#### Scenario: Re-ask after resume

- **WHEN** an agent killed on a question is resumed
- **THEN** its first turn carries the quoted question and the re-ask instruction, and the existing needs-input detection surfaces it once asked

#### Scenario: Coordinator note lists team

- **WHEN** a coordinator is resumed and 4 of its workers were killed
- **THEN** the note names each with its state and says to revive them with hera_revive

### Requirement: Off switch restores prior behavior

`[bounce_recovery] enabled = false` SHALL make the daemon behave exactly as before this change: no ledger writes, no resumes, no notes. The setting SHALL default to enabled. The legacy in-process (supervisor off) path SHALL be unchanged.

#### Scenario: Disabled

- **WHEN** enabled is false and a supervisor restart orphans agents
- **THEN** they are reconciled to in_review and signalled with ARGUS_BOUNCED only, as before

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

