## MODIFIED Requirements

### Requirement: A merge transition nudges the task's Hera coordinator

When a poll cycle resolves a task's PR as genuinely MERGED (not merely closed – the cached `state` field persists both as the single terminal value `merged-closed`, so the poller SHALL additionally track the raw merged/closed distinction via `gitutil.PRResult.Merged` to tell them apart) and the task holds (or has ever held) a Hera role, the system SHALL send TWO independent notifications, neither skipped because the other fired:

1. **Coordinator nudge** (unchanged): the system SHALL send that task's resolved orchestrator coordinator a nudge message – never a status change, never an automatic accept/complete. The nudge names the task and includes the PR URL, framed as "worth reviewing/accepting" rather than as a completed fact. This is a SILENT no-op when the PR was closed WITHOUT being merged; when the task never held a Hera role at all; when no coordinator role resolves for that orchestrator; or when the resolved role IS ITSELF the coordinator (its own PR merged – it does not need to be told via this channel).
2. **Role self-assessment notice** (new): the system SHALL ALSO send a message directly to the task's own resolved Hera role (regardless of role kind, while the task is neither complete nor archived), telling it the PR merged and asking it to decide, using its own judgment, whether it has any further tasks – if not, to inform its coordinator and mark itself complete via its own existing tools. This introduces no new completion primitive and never flips the task's status or hera role-status itself; the daemon only delivers information. This is a SILENT no-op when the task never held a Hera role, when no coordinator role resolves (needed to identify the notice's sender), or when the resolved role IS the coordinator (a message cannot be sent from a role to itself).

The system SHALL resolve the task's most recent Hera role (any binding, live or ended) and that role's orchestrator's coordinator identically for both notifications, mirroring the existing coordinator-resolution pattern the plan-DAG gater already uses for its own hold/fan-in notices.

Merge notifications SHALL fire only when the pre-poll cached state was non-terminal or absent. Refreshing an already terminal state to verify its lifecycle SHALL NOT repeat either notification. Verified terminal entries are excluded from subsequent polls.

Derived from: `internal/daemon/daemon.go` (`pollPRStatesOnce`, `resolveHeraRoleAndCoordinator`, `notifyCoordinatorOfMergedPR`, `notifyRoleOfMergedPR`), `internal/gitutil/pr_batch.go` (`PRResult.Merged`, `mapBatchNode`), `internal/db/hera_messages.go` (`SendHeraMessage`'s `ErrHeraMessageSelfSend` guard, the reason the role notice cannot reach a coordinator's own directly-bound task).

#### Scenario: A genuine merge transition nudges the coordinator

- **WHEN** a poll cycle resolves a task's PR as MERGED (`PRResult.Merged` true) for a task that holds (or has held) a Hera worker role, and a coordinator resolves for that role's orchestrator
- **THEN** the coordinator role receives a nudge message naming the task and its PR URL; the task's argus status and hera role-status are both left completely unchanged

#### Scenario: A genuine merge transition also asks the role to self-assess

- **WHEN** a poll cycle resolves a task's PR as MERGED (`PRResult.Merged` true) for a task whose most recently bound Hera role is NOT the coordinator itself, and a coordinator resolves for that role's orchestrator
- **THEN** the role receives a message naming the merged PR's URL and instructing it to inform its coordinator and mark itself complete if it has no further tasks; the coordinator's own nudge from the scenario above is also delivered, unaffected

#### Scenario: An in-progress role still receives the notice

- **WHEN** a poll cycle resolves a task's PR as MERGED (`PRResult.Merged` true) for a task whose most recently bound Hera role is worker-kind and whose task status is `in_progress` (not `in_review`)
- **THEN** the role still receives the self-assessment notice; the task remains eligible because it is neither complete nor archived

#### Scenario: A coordinator's own merged PR gets neither notification

- **WHEN** a poll cycle resolves a task's PR as MERGED (`PRResult.Merged` true) for a task whose most recently bound Hera role IS the coordinator itself
- **THEN** neither the coordinator nudge (self-skipped) nor the role self-assessment notice (blocked by the messaging layer's self-send rejection) is delivered

#### Scenario: An unmerged close never triggers either notification

- **WHEN** a poll cycle resolves a task's PR state to `merged-closed` but `PRResult.Merged` is false
- **THEN** neither the coordinator nudge nor the role self-assessment notice is sent

The role notice SHALL carry durable daemon provenance committed atomically with the message. The system SHALL re-read task status and archive state under the send transaction lock, skipping complete or archived tasks even when the poller snapshot predates completion. Coordinator nudges SHALL remain unchanged.

#### Scenario: Completion precedes poller observation

- **WHEN** a merge is observed after the worker task became complete or archived
- **THEN** no role notice is stored or delivered, and the coordinator nudge remains independent

#### Scenario: Completion races a stale snapshot

- **WHEN** the poller snapshot is in review but the database task is complete before notice insertion
- **THEN** the send transaction skips the role notice
