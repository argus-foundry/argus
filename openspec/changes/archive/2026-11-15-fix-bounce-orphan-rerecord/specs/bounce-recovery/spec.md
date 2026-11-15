## MODIFIED Requirements

### Requirement: Recovery ledger of bounce-killed sessions

When the supervisor-restart reconcile finds true orphans, the daemon SHALL record each non-resident, non-archived, non-complete orphan that is not ready_to_close in a durable ledger (task_meta namespace `bounce`) before any resume is attempted. Orphan selection SHALL follow the recovery-candidates liveness requirement. Each entry SHALL carry the kill time, the agent's state at death (`parked_question`, `mid_turn`, `idle`, or `unknown`), the pending question text and options when parked, the hera role kind and orchestrator, and a recovery status (`pending`, `resumed`, `dormant`, `delegated`, `failed`, `expired`, `superseded`). The ledger SHALL survive a daemon restart so an interrupted recovery continues. Eligible idle team members SHALL be recorded as dormant only when current session-loss evidence identifies them as killed by this bounce; team membership and session absence alone SHALL NOT create or replace an entry. Each entry SHALL persist whether its resume note was delivered. Finished entries older than `expiry_hours` SHALL be deleted. When the ledger cannot be written, the daemon SHALL log the failure and continue serving. Reconciliation of a previously recorded death SHALL preserve the entire existing entry, including original killed_at, attempts, bounce_id, recovery status and note delivery state. A genuinely resumed session lost in a later bounce SHALL receive a fresh entry. Repeated reconciliation of the same bounce SHALL be idempotent and SHALL NOT reset recovery progress.

#### Scenario: Orphans recorded before resume

- **WHEN** a supervisor restart leaves live-at-shutdown tasks with stale pending or in_review statuses and no live session
- **THEN** each eligible orphan gets a ledger entry with its kill time, state and role kind before the first resume starts

#### Scenario: Ledger survives daemon bounce

- **WHEN** the daemon restarts while entries are still pending or already have recovery progress
- **THEN** the recovery worker continues from the stored entries without re-classifying or rewriting them

#### Scenario: Dead teammate already recorded

- **WHEN** a newly interrupted coordinator has a dead bound teammate with an older ledger entry but no current session-loss evidence
- **THEN** that teammate's entire entry remains unchanged

#### Scenario: Later genuine interruption

- **WHEN** a previously recovered task resumes and a later supervisor shutdown records its live session before killing it
- **THEN** the later bounce creates a fresh entry with its new kill time and recovery budget

#### Scenario: Same bounce reconciled twice

- **WHEN** startup reconciliation repeats for the same session loss within seconds
- **THEN** it preserves recorded recovery progress and sends no duplicate ARGUS_BOUNCED

### Requirement: Recovery candidates follow session liveness

With bounce recovery enabled, supervisor-mode reconciliation SHALL consider a dead task a new orphan only when it was recorded running before the current supervisor shutdown or has an unreconciled in_progress status transition. A durable live Hera binding alone SHALL NOT establish session loss. It SHALL exclude residents, archived/complete tasks and ready_to_close tasks. It SHALL leave supervisor-confirmed live sessions untouched and SHALL skip reconciliation when the supervisor live-set query fails. Snapshot evidence SHALL be consumed after successful reconciliation. Already-dead pending/in_review tasks without current session-loss evidence SHALL retain existing ledger entries and SHALL receive no new ARGUS_BOUNCED. The daemon SHALL log skipped historical candidates. Idle workers identified as newly lost SHALL remain dormant even when their coordinator is also recovering. The legacy in-process path and disabled-recovery status-only reconciliation SHALL remain unchanged.

#### Scenario: Stale status on a live agent

- **WHEN** the supervisor recorded a pending plain task and an in_review coordinator as running before shutdown
- **THEN** both dead sessions enter recovery regardless of those statuses

#### Scenario: Daemon-only restart with dead bindings

- **WHEN** the supervisor remains alive across a daemon restart and pending/in_review tasks have durable Hera bindings but no session-loss evidence
- **THEN** those tasks are not new orphans, their ledger entries remain unchanged and no new ARGUS_BOUNCED is sent

#### Scenario: Crash fallback

- **WHEN** a supervisor crash leaves no shutdown snapshot and a task remains in_progress without a live session
- **THEN** that unreconciled transition enters recovery, while binding-only pending/in_review tasks remain available for explicit revival

#### Scenario: Idle worker remains dormant

- **WHEN** an orphaned worker was idle between turns and its coordinator is recovering
- **THEN** it is recorded dormant and listed in the coordinator note without automatic revival
