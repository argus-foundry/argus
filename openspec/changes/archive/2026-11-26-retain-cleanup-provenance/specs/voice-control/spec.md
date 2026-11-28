## ADDED Requirements

### Requirement: Reconcile a delayed archive response with current state

A delayed Archive or Unarchive completion SHALL use the task's authoritative current archival state before closing a newer detail visit.

#### Scenario: Unarchive succeeds before the older Archive response arrives
- **WHEN** the user has reopened the currently active task after Unarchive
- **THEN** the older Archive response leaves the active terminal open.
