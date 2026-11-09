## ADDED Requirements

### Requirement: Runtime independent of workflow
The workspace SHALL use authoritative live/idle flags for busy indication and input readiness regardless of workflow status. Archive and pending-archive guards SHALL remain authoritative. session.runtime_changed SHALL apply runtime fields and request a fresh task snapshot. Absent sessions SHALL not be labeled idle.

#### Scenario: Busy task after completion
- **WHEN** an in_review or complete task still has a busy live session
- **THEN** its busy icon remains visible and input follows live state and archive guards

### Requirement: Explicit standalone failure
The workspace SHALL combine task.failure with Hera role failure, render a plain reason in Info, and clear stale failure on task.failure_changed. It SHALL NOT parse opaque raw Result text or infer reported failure from process exit.

#### Scenario: Failure clears
- **WHEN** task.failure_changed carries failure null
- **THEN** the old standalone failure icon and reason disappear while any remaining Hera failure is preserved
