## MODIFIED Requirements

### Requirement: Resident timer management

The system SHALL expose resident-only MCP tools to set a one-shot timer by absolute RFC3339 time or positive duration, schedule recurrence using cron or a simple expression with an IANA local timezone, list active timers, and cancel personal timers by id. Config-owned built-in timers SHALL be identified by a `builtin` field in tool and REST listings and SHALL refuse agent cancellation. The authenticated resident identity guard MUST refuse other callers. The store SHALL cap active personal timers at 64 (excluding built-ins) and personal notes at 4096 bytes, reject blank notes and past one-shots, and require recurring intervals of at least 15 minutes.

#### Scenario: Own follow-up

- **WHEN** the authenticated resident sets a timer with a note and future time
- **THEN** the durable timer records an id, note, creation time and next due time

#### Scenario: Foreign caller

- **WHEN** an unbound or non-resident caller invokes any timer tool
- **THEN** the call is refused without reading or modifying timers

#### Scenario: Bounds

- **WHEN** a personal timer exceeds the active personal count, note size or recurrence frequency bounds
- **THEN** creation fails without inserting a timer

#### Scenario: Built-in timer

- **WHEN** Solo lists timers and attempts to cancel the memory tidy built-in
- **THEN** the listing identifies its ownership and cancellation is refused with config guidance
