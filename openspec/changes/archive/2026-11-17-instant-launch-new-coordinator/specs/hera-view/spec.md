# Hera View

## ADDED Requirements

### Requirement: New root coordinator launches instantly with a pending row

The system SHALL, on submit of the new-coordinator form, immediately overlay an optimistic pending coordinator row on the rail (a synthetic orchestrator carrying one coordinator role, never written to the store), select that row, move focus to the coordinator pane, and show the shared LAUNCH banner there, before the slow worktree and session creation finishes. Creation SHALL continue to run off the UI goroutine. The overlay SHALL be applied on the UI goroutine to every rail model install (synchronous and asynchronous rebuilds), so a background refresh never drops the pending row, and SHALL NOT affect the change-detection equality state of the real model.

#### Scenario: Pending row appears and is selected on submit

- **WHEN** the operator submits the new-coordinator form
- **THEN** the rail shows a pending coordinator row selected under the Active group, the coordinator pane shows the LAUNCH banner, and focus is on the coordinator pane, all before creation completes

#### Scenario: Completion lands focus on the live coordinator

- **WHEN** creation succeeds and the pending row is still the selected row
- **THEN** the pending row is replaced by the real orchestrator, the real row is selected, and focus is on the coordinator pane showing the live agent

#### Scenario: Navigating away suppresses the focus change

- **WHEN** the operator moves the rail selection off the pending row before creation completes
- **THEN** on completion the rail refreshes with the real orchestrator but the selection and focus are not moved

#### Scenario: Failure removes the pending row

- **WHEN** creation fails
- **THEN** the pending row is removed, the error is shown in the status bar, and no orchestrator, role, or task row is left behind

#### Scenario: Pending row is inert to mutations

- **WHEN** the operator presses a rail mutation key (nuke, rename, spawn, hide, pin, status step, bounce) on the pending row
- **THEN** the key is consumed, nothing is written, and the status bar says the coordinator is still launching
