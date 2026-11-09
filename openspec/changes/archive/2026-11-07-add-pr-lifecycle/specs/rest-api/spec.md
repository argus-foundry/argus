## ADDED Requirements

### Requirement: Task PR lifecycle projection

`GET /api/tasks` and `GET /api/tasks/{id}` SHALL include `pr_lifecycle` (`open|merged|closed|unknown`) from the cached `pr` namespace without invoking GitHub. A merged-closed `pr_state` with no verified terminal lifecycle (including a contradictory `open`), a missing PR, or any unrecognized value SHALL project `unknown`. `pr_state` semantics are unchanged.

#### Scenario: Closed and merged differ

- **WHEN** two tasks have `pr_state` `merged-closed` with lifecycles `closed` and `merged`
- **THEN** their entries report `pr_lifecycle` `closed` and `merged`
