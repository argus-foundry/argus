## MODIFIED Requirements

### Requirement: Coordinators proactively archive done-done roles

The coordinator orientation and the builtin `hera` skill SHALL instruct coordinators to call `hera_archive` for roles that are done-done: after accepting a role and receiving its winding-down confirmation, after a merged-PR notice for a role's work, and as a sweep before reporting an orchestrator finished. They SHALL also instruct coordinators that a worker whose PR is open, green, and pushed with a clean worktree may be archived without waiting for the merge (a handed-off archive), that `force=true` with a `reason` is the coordinator-only escape hatch (never used in a sweep), that `hera_unarchive` hands a role back, and that a closed-unmerged PR of a handed-off role brings it back with a board decision item. They SHALL state that refusals carry reasons the coordinator should resolve or leave in place, and that archive does not delete worktrees.

#### Scenario: Orientation mentions the archive duty and handoff

- **WHEN** the coordinator orientation or `hera` skill is rendered
- **THEN** it includes guidance to archive done-done roles with `hera_archive`, to sweep before declaring done, and to use handed-off archive, `force`+`reason`, and `hera_unarchive`

## ADDED Requirements

### Requirement: hera_unarchive coordinator tool

The system SHALL provide a coordinator-only `hera_unarchive(cwd, role_name, [orchestrator])` MCP tool with `hera_accept`'s authorization shape that un-archives a previously archived worker or freelance role in the caller's orchestrator: it un-archives the task, un-hides the role, rolls the task to `in_progress` through the exact-status-gated revive, and settles the role's unsettled archive record as handed back, in one transaction. It SHALL NOT start a session or perform git checks, and SHALL refuse a role that is not archived.

#### Scenario: Handed-off role is brought back

- **WHEN** a coordinator calls `hera_unarchive` for an archived role
- **THEN** the role is visible again and its task is `in_progress`

#### Scenario: Not archived refuses

- **WHEN** the role is not archived
- **THEN** the tool returns an error and writes nothing
