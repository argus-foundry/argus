## ADDED Requirements

### Requirement: Action and board todo parity in Decide

The SPA board, shared Decide rail and standalone /decide page SHALL expose Actions and Todos alongside Decisions and FYIs, with per-kind counts and all-kind launch focus. Pending items and board todos SHALL remain accessible without contributing to human-attention badges. The common priority order SHALL apply within each filtered view. New cards SHALL show trigger/readiness, action blocking state and an exact copy control for command/step text. Action controls SHALL offer Done; board todo controls SHALL offer Done and Dropped; optional note SHALL never be required. The page SHALL NOT execute stored steps. The existing decision/FYI shortcuts SHALL retain their behavior; every tab SHALL be accessible by visible controls, normal keyboard focus and adjacent-tab phone swipes.

#### Scenario: Open action from the TUI

- **WHEN** Aaron opens the selected action in the browser
- **THEN** its coordinator and Actions tab are selected with the exact step, Copy and Done visible

#### Scenario: Phone future work

- **WHEN** Aaron selects Todos at phone width
- **THEN** deferred triggers, Done/Dropped and optional note remain reachable without horizontal overflow or keyboard shortcuts

### Requirement: Receipt-safe completion through the shared queue

Done/Dropped actions SHALL use the existing global response queue, review/copy summary, guarded draft persistence, per-item revision checks, role-batched submissions and durable receipts. Failed or uncertain submissions SHALL retain drafts and retry the identical action ID and frozen payload. Accepted completions SHALL clear only confirmed drafts and count toward Submit & close completion. A stale revision SHALL preserve notes while requiring explicit reconfirmation. Existing decision answers, FYI comments/dismissal and annotation behavior SHALL remain unchanged; annotation creation for the new kinds is outside this change.

#### Scenario: Mixed round

- **WHEN** Aaron queues a decision answer, FYI comment, action Done and board todo Dropped
- **THEN** both submit controls process every queued operation with correct summaries and only confirmed results clear drafts

#### Scenario: Uncertain completion retry

- **WHEN** the daemon commits Done but the response is lost before reload
- **THEN** the saved action ID/payload replay the stored receipt without another completion or coordinator notice

#### Scenario: Stale action

- **WHEN** the coordinator changes an action's step after Aaron queues Done
- **THEN** submission reports conflict, preserves the optional note and requires confirmation of the new revision
