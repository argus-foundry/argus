## ADDED Requirements

### Requirement: Coordinators proactively archive done-done roles

The coordinator orientation and the builtin `hera` skill SHALL instruct coordinators to call `hera_archive` for roles that are done-done: after accepting a role and receiving its winding-down confirmation, after a merged-PR notice for a role's work, and as a sweep before reporting an orchestrator finished. They SHALL state that refusals carry reasons the coordinator should resolve or leave in place, and that archive does not delete worktrees.

#### Scenario: Orientation mentions archive duty

- **WHEN** a coordinator's orientation text is generated
- **THEN** it includes guidance to archive done-done roles with `hera_archive` and to sweep before declaring done
