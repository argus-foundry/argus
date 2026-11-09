## ADDED Requirements

### Requirement: Show measured current session totals and paginated turns alongside measured Codex context.

The context ring uses GET /context measurements for Codex, including recorded and unavailable states. Session usage reads GET /usage with limit=50, pins older pages to returned session_id, preserves nullable counters and labels cached input as included in input. task.usage_changed refreshes measurements. Session changes and task changes cancel and fence old reads.

#### Scenario: Obsolete responses

- **WHEN** a task or connection changes while a request is pending
- **THEN** late responses cannot overwrite the current view or draft

#### Scenario: Unavailable API

- **WHEN** a read or mutation fails
- **THEN** the view shows actionable retry feedback without claiming success
