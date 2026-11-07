## ADDED Requirements

### Requirement: Hera lifecycle operation events

The system SHALL emit `hera.lifecycle_changed` with payload `{operation_id, role_id, task_id, kind, state, error}` after each persisted lifecycle operation transition, including acceptance, for the operation's task.

#### Scenario: Bounce accepted
- **WHEN** a bounce operation is accepted
- **THEN** one event with `state:"awaiting_handoff"` and `error:null` is emitted
