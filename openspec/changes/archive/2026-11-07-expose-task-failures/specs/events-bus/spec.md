## ADDED Requirements

### Requirement: task.failure_changed event

The system SHALL publish `task.failure_changed` with payload `{"failure":null}` or the complete failure object after a Result write changes the failure projection. Clearing or replacing a failed Result SHALL publish it; a write that neither starts from nor ends at a failure, and a no-op write, SHALL NOT.

#### Scenario: Failure cleared

- **WHEN** a failed Result is replaced by a non-failure Result
- **THEN** one event with `{"failure":null}` is published
