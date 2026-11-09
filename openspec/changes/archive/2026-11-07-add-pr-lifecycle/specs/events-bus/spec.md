## ADDED Requirements

### Requirement: task.pr_changed event

The system SHALL publish `task.pr_changed` with the task id and payload `{"url","state","lifecycle"}` after a changed PR cache commit and outside the database mutex. Consumers SHALL ignore payload keys they do not know.

#### Scenario: Payload shape

- **WHEN** a merged PR is first cached
- **THEN** the payload is `{"url":"...","state":"merged-closed","lifecycle":"merged"}`
