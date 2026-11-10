## MODIFIED Requirements

### Requirement: The Stop hook consults the daemon only for the resident

The resident's spawn environment SHALL carry `ARGUS_RESIDENT=1` alongside `ARGUS_TASK_ID`, and no other task's SHALL. The coord-hook SHALL ask the daemon whether the task is the resident only when that variable is set, so plain tasks incur no added Stop-time RPC. Because the resident now holds a Hera coordinator role, the hook SHALL NOT use the presence of a Hera role to skip or to select resident behavior: for the resident the resident path (reply capture, `context_size` stamp, budget nudge) runs, and the generic coordinator budget, nudge and recycle-request behavior SHALL NOT apply to it. Hera coordinator, worker and freelance behavior for every other task SHALL be unchanged.

#### Scenario: Plain task Stop

- **WHEN** a task without `ARGUS_RESIDENT` and without a Hera role stops
- **THEN** the coord-hook makes no resident lookup and exits silently

#### Scenario: Resident Stop with a Hera role

- **WHEN** the resident, which holds a coordinator role, stops
- **THEN** the hook records the reply and context size through the resident path and does not also apply the coordinator budget nudge

#### Scenario: Other coordinators unchanged

- **WHEN** a regular coordinator stops
- **THEN** the coordinator nudge and stamp behave exactly as before
