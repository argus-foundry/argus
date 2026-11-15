# Resident timers specification

## Purpose

Give the resident durable, purposeful follow-up and recurring wake-ups through its own inbox without creating tasks or polling external sources.

## Requirements

### Requirement: Resident timer management

The system SHALL expose resident-only MCP tools to set a one-shot timer by absolute RFC3339 time or positive duration, schedule recurrence using cron or a simple expression with an explicit IANA local timezone, list active timers, and cancel personal timers by id. The implicit `Local` timezone MUST be rejected case-insensitively. Config-owned built-in timers SHALL be identified by a `builtin` field in tool and REST listings and SHALL refuse agent cancellation. The authenticated resident identity guard MUST refuse other callers. The store SHALL cap active personal timers at 64 (excluding built-ins) and personal notes at 4096 bytes, reject blank notes and past one-shots, and require recurring intervals of at least 15 minutes.

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

#### Scenario: Implicit timezone refused

- **WHEN** a recurrence requests `Local` in any letter case
- **THEN** the request is rejected and the caller must supply an explicit IANA zone

### Requirement: Durable coalesced timer delivery

The daemon SHALL atomically create exactly one system inbox message for each due timer and advance or complete it. The message MUST start with `[argus] ` and carry timer id, original note, creation time and `was due at` time. Delivery SHALL use the existing idle-gated reliable resident path, retry while unavailable, and survive daemon bounce, supervisor restart and resident recycle. A missed recurrence SHALL produce one late fire and advance strictly past now, never replay all missed occurrences. A recurrence with an unread earlier event SHALL coalesce until that event is acknowledged, including after notifier submission. A submitted marker SHALL survive daemon bounce so an unread event is not woken twice. Message creation SHALL emit the normal message-sent event after its transaction commits.

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

#### Scenario: Submitted event remains unread

- **WHEN** another recurrence is due after its prior wake-up was submitted but its message remains unread
- **THEN** no additional inbox event or wake-up is created, and the next due time advances past now

#### Scenario: Inbox event deleted

- **WHEN** a receipt refers to a message that no longer exists
- **THEN** the stale registration and receipt are cleared and the timer can fire again

### Requirement: Human timer visibility

The REST API SHALL list active resident timers, including notes, creation time, schedule, timezone and next due time. Lifecycle logs SHALL use `[resident-timer]` for set, fire, deliver, skip and cancel without logging note contents.

#### Scenario: Inspect timers

- **WHEN** the human requests the resident timer REST endpoint
- **THEN** it returns the same active timers managed by resident tools

### Requirement: Timer failure isolation and retention

A timer whose next recurrence cannot be calculated SHALL be deactivated and logged without blocking healthy timers or delivery retries. The daemon SHALL process pending deliveries and cancellations even if firing reports an error. Terminal timer rows older than 30 days SHALL be pruned once their delivery receipts are settled; undelivered events MUST survive extended downtime. Timer state-read errors SHALL be logged.

#### Scenario: Broken recurrence alongside healthy work

- **WHEN** a due recurring timer has an invalid zone or no next occurrence
- **THEN** it is deactivated and other timers, pending deliveries and cancellations continue

#### Scenario: Old terminal history

- **WHEN** a completed or cancelled timer has been terminal for more than 30 days and has no unsettled receipt
- **THEN** its timer row is deleted while active and unsettled timers are preserved
