## ADDED Requirements

### Requirement: session.runtime_changed event

The system SHALL publish `session.runtime_changed` with payload `{"live":bool,"idle":bool,"needs_input":bool}` and the task id whenever the task's REST runtime flags change, including changes caused only by a workflow-status transition. A task without a previous value SHALL be treated as all-false, and unchanged flags SHALL emit nothing.

#### Scenario: Pending restart clears live

- **WHEN** a live task enters a pending restart
- **THEN** one event with all flags false is published
