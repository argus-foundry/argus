# bounce-recovery Specification

## Purpose
After a supervisor restart kills agent sessions, the daemon records what each was doing, resumes the interrupted ones in throttled coordinator-first order, and tells each what happened with an [argus] note.
## Requirements
### Requirement: Recovery ledger of bounce-killed sessions

When the supervisor-restart reconcile finds true orphans, the daemon SHALL record each non-resident, non-archived orphan in a durable ledger (task_meta namespace `bounce`) before any resume is attempted. Each entry SHALL carry the kill time, the agent's state at death (`parked_question`, `mid_turn`, `idle`, or `unknown`), the pending question text and options when parked, the hera role kind and orchestrator, and a recovery status (`pending`, `resumed`, `dormant`, `delegated`, `failed`, `expired`, `superseded`). The ledger SHALL survive a daemon restart so an interrupted recovery continues. When the ledger cannot be written, the daemon SHALL log the failure and continue serving.

#### Scenario: Orphans recorded before resume

- **WHEN** a supervisor restart leaves 27 in-progress tasks with no live session
- **THEN** each one gets a ledger entry with its kill time, state and role kind before the first resume starts

#### Scenario: Ledger survives daemon bounce

- **WHEN** the daemon restarts while entries are still `pending`
- **THEN** the recovery worker continues from the stored entries without re-classifying already-resumed ones

### Requirement: State at death is read from the transcript

The daemon SHALL classify a Claude-family orphan from the tail of its session transcript, after the session ID refresh. An unanswered AskUserQuestion or ExitPlanMode tool call SHALL classify as `parked_question`. A last entry that is an assistant tool call, a tool result, a thinking block, a partial assistant message, or a user message with no assistant reply SHALL classify as `mid_turn`. A last assistant message that ended its turn SHALL classify as `idle`. A missing or unreadable transcript, or a non-Claude backend, SHALL classify as `unknown`. Classification SHALL read a bounded tail and SHALL never block the reconcile.

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

The recovery worker SHALL resume: every orphaned coordinator regardless of state; and every orphaned worker, freelance or plain task that has no live coordinator above it and whose state is `parked_question`, `mid_turn` or `unknown`. It SHALL NOT resume a worker or sub-coordinator whose coordinator is itself being resumed (status `delegated`: the coordinator revives it with `hera_revive`). It SHALL leave idle-between-turns agents with no coordinator dormant. It SHALL NOT resume residents, archived tasks, or tasks the user stopped (not orphans). If a delegating coordinator cannot be resumed, its killed workers SHALL be re-evaluated as unsupervised.

#### Scenario: Worker under a resumed coordinator

- **WHEN** a worker and its coordinator were both killed and the coordinator is resumable
- **THEN** the worker is `delegated` and the daemon does not start it

#### Scenario: Idle plain task

- **WHEN** a plain non-hera task was idle between turns at the bounce
- **THEN** it stays dormant and is not resumed

#### Scenario: Coordinator cannot be resumed

- **WHEN** the coordinator's task is archived or has no resumable session
- **THEN** its killed mid-turn workers are resumed directly

### Requirement: Throttled, coordinator-first resume

The recovery worker SHALL start resumes in order of orchestrator depth (shallowest coordinators first, then unsupervised agents), oldest kill first within a tier. It SHALL keep at most `max_concurrent` resumed sessions unsettled at once and SHALL wait at least `stagger_seconds` between starts. A resumed session counts as settled once it is idle at least 8 seconds after its start, or after a 60 second ceiling. The worker SHALL wait for the daemon's MCP port to be bound (unless the MCP server is disabled) before starting any resume. A failed start SHALL be retried with backoff up to 3 attempts, then marked `failed` and logged. Entries older than `expiry_hours` SHALL be marked `expired` and never resumed.

#### Scenario: Thirty orphans

- **WHEN** 30 agents are eligible
- **THEN** no more than `max_concurrent` are starting at any time and coordinators start before plain tasks

#### Scenario: Stale ledger

- **WHEN** the daemon was down longer than `expiry_hours`
- **THEN** entries are marked `expired` and nothing is resumed

### Requirement: Resume note delivered as an argus system turn

After a resumed session starts, the daemon SHALL deliver a note beginning with `[argus] ` through the idle-gated reliable-notify path so it is never mistaken for the user's message. The note SHALL state that a supervisor restart killed the session at the recorded time and what it was doing. For `parked_question` it SHALL quote the pending question and options as data and instruct the agent to re-ask it with AskUserQuestion. For a coordinator it SHALL list each killed worker and sub-coordinator with name, kind and state at death, and instruct it to resume them with `hera_revive` in waves of at most 3, busy ones first, and to post any open question to its board. Question text SHALL be quoted data, never instructions.

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

The recovery worker SHALL skip an entry whose task now has a live session, is archived, is complete, or was resumed by someone else since the bounce, and SHALL log the skip. It SHALL never kick, stop or signal a live session.

#### Scenario: Human resumed first

- **WHEN** Aaron presses Enter on a dead pane before the worker reaches its entry
- **THEN** the worker skips it as superseded

