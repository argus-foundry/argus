## ADDED Requirements

### Requirement: Task failure projection

`GET /api/tasks` and `GET /api/tasks/{id}` SHALL include `failure`, `null` or `{"reason","source":"task_result","recorded_at"}`, derived from a Result object whose `failed` is boolean `true`. `recorded_at` SHALL be the persisted time the result last changed and SHALL be omitted when none was persisted. `reason` SHALL be bounded to 4 KiB. A malformed, non-object or unrelated Result SHALL yield `failure: null` and never an error. User stop, transport loss and nonzero exit SHALL NOT produce a failure. Standalone and Hera-bound tasks SHALL share the projection.

#### Scenario: Explicit failed result

- **WHEN** a task's Result is `{"failed":true,"reason":"validation failed"}`
- **THEN** its entry reports that reason, source `task_result` and the persisted `recorded_at`

#### Scenario: Malformed result

- **WHEN** a task's Result is not valid JSON
- **THEN** the request succeeds with `failure: null`
