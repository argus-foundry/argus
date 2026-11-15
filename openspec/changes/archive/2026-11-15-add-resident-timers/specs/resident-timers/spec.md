## ADDED Requirements

### Requirement: Resident timer management

The system SHALL expose resident-only MCP tools to set a one-shot timer by absolute RFC3339 time or positive duration, schedule recurrence using cron or a simple expression with an IANA local timezone, list active timers, and cancel by id. The authenticated resident identity guard MUST refuse other callers. The store SHALL cap active timers at 64 and notes at 4096 bytes, reject blank notes and past one-shots, and require recurring intervals of at least 15 minutes.

#### Scenario: Own follow-up

- **WHEN** the authenticated resident sets a timer with a note and future time
- **THEN** the durable timer records an id, note, creation time and next due time

#### Scenario: Foreign caller

- **WHEN** an unbound or non-resident caller invokes any timer tool
- **THEN** the call is refused without reading or modifying timers

#### Scenario: Bounds

- **WHEN** a timer exceeds the active count, note size or recurrence frequency bounds
- **THEN** creation fails without inserting a timer

### Requirement: Durable coalesced timer delivery

The daemon SHALL atomically create exactly one system inbox message for each due timer and advance or complete it. The message MUST start with `[argus] ` and carry timer id, original note, creation time and `was due at` time. Delivery SHALL use the existing idle-gated reliable resident path, retry while unavailable, and survive daemon bounce, supervisor restart and resident recycle. A missed recurrence SHALL produce one late fire and advance strictly past now, never replay all missed occurrences. A recurrence with a pending earlier fire SHALL coalesce until that fire is submitted or acknowledged.

#### Scenario: Busy or absent resident

- **WHEN** a timer becomes due while Solo is busy, down or recycling
- **THEN** one inbox message remains durable and its wake-up waits for the resident's safe idle window

#### Scenario: Downtime

- **WHEN** the daemon resumes after several recurrence times passed
- **THEN** one late message is created with the original due time and the next recurrence is after now

#### Scenario: Local daylight saving

- **WHEN** recurrence crosses daylight saving transitions
- **THEN** next due times follow the specified timezone, skipping nonexistent local times

#### Scenario: Notes contain terminal controls

- **WHEN** a timer note contains newlines or terminal control characters
- **THEN** the inbox preserves the original note and the wake-up quotes it as data on one terminal line

### Requirement: Human timer visibility

The REST API SHALL list active resident timers, including notes, creation time, schedule, timezone and next due time. Lifecycle logs SHALL use `[resident-timer]` for set, fire, deliver, skip and cancel without logging note contents.

#### Scenario: Inspect timers

- **WHEN** the human requests the resident timer REST endpoint
- **THEN** it returns the same active timers managed by resident tools
