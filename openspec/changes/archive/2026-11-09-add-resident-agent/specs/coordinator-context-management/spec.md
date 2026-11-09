## ADDED Requirements

### Requirement: The Stop hook also acts for the resident task

The `argus coord-hook` Stop hook SHALL treat the resident task as an additional eligible subject alongside hera-bound roles: for a task whose kind is `resident` it SHALL stamp `context_size` on every Stop, record the resident reply state, and, when `context_size` is at or above `resident.context_budget`, emit the existing non-blocking nudge subject to the existing growth-increment gate. The resident nudge text SHALL instruct the agent to write undone items to `resident/open-loops.md`, call `task_recycle` with a short handoff note, and not do so while it has background work or a turn it still owes the user. For the resident the hook SHALL NOT use the hera 1.5x hard-stop path and SHALL NOT require a hera role. Behavior for hera-bound roles and for all other tasks SHALL be unchanged.

#### Scenario: Resident stamps context without a binding

- **WHEN** the Stop hook fires for the resident task, which has no hera role binding
- **THEN** it stamps `context_size` and does not no-op

#### Scenario: Over budget nudges with resident wording

- **WHEN** the resident's `context_size` reaches `resident.context_budget`
- **THEN** the hook emits the non-blocking nudge naming `resident/open-loops.md` and `task_recycle`

#### Scenario: No hard-stop for the resident

- **WHEN** the resident's `context_size` reaches 1.5x its budget
- **THEN** the hook still only nudges (subject to the growth gate) and calls no forced-recycle RPC

#### Scenario: Other non-hera sessions still no-op

- **WHEN** the Stop hook fires for a plain task with no hera role binding
- **THEN** it exits with no side effects as before
