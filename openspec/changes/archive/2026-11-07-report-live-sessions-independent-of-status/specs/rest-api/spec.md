## MODIFIED Requirements

### Requirement: Task listing and retrieval with runtime idle state

The server SHALL list tasks and SHALL support optional filtering by `status`, `project`, and `archived` (`0`/default excludes archived, `1` returns only archived, `all` returns both). For each task the server SHALL derive `live`, true whenever the runner holds an input-addressable session and no kill+resume restart is pending, regardless of workflow status, and `idle`, true only when the session is live and waiting for input; an absent session SHALL NOT be reported idle. A pending restart SHALL report `live`, `idle` and `needs_input` all false. `needs_input` SHALL require a live `in_progress` session. Retrieving a task that does not exist SHALL return 404.

#### Scenario: Live busy in_review task

- **WHEN** an `in_review` task has a live, non-idle session
- **THEN** its entry reports `live: true` and no `idle`

#### Scenario: Absent session is not idle

- **WHEN** a task of any status has no session
- **THEN** its entry reports `live: false` and no `idle`

#### Scenario: Pending restart never advertises input

- **WHEN** a live task has a pending kill+resume restart
- **THEN** its entry reports `live: false`, no `idle` and no `needs_input`
