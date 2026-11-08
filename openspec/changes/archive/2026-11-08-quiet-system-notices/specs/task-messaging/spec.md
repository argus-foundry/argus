## MODIFIED Requirements

### Requirement: Message caps

The system SHALL reject messages that violate protective caps and surface each cap rejection as a bad-request error with the reason preserved. The caps SHALL cover an oversized body, a self-send (sender equals recipient), a full recipient inbox, and exceeding the send rate limit. The unread count that drives the full-inbox cap SHALL exclude daemon-originated system notes, and system-note inserts SHALL never be rejected as inbox-full.

#### Scenario: Self-send rejected

- **WHEN** a client sends a message whose recipient is the same as the sender task
- **THEN** the request fails with a bad-request error indicating a self-send

#### Scenario: Oversized body rejected

- **WHEN** a client sends a message whose body exceeds the maximum body size
- **THEN** the request fails with a client error rather than a server error

#### Scenario: System notes do not fill the inbox cap

- **WHEN** a recipient holds 500 unread system notes and a real task sends it a message
- **THEN** the message is accepted rather than rejected as inbox-full

## ADDED Requirements

### Requirement: System notes are not unread mail

Daemon-originated notes (sender `argus:system`) SHALL NOT be counted as unread mail on any human-facing surface: the REST inbox `unread_count`, the Claude bridge band and inbox command, and the TUI inbox viewer. They SHALL remain stored and returned by the inbox listing so an agent can still read them.

#### Scenario: REST unread count ignores system notes

- **WHEN** a task has three unread system notes and one unread note from another task
- **THEN** the REST inbox `unread_count` is 1 and the listing still returns all four messages

#### Scenario: TUI inbox shows system notes as not unread

- **WHEN** the TUI inbox lists an unread system note
- **THEN** its row is not rendered as unread

### Requirement: System notes coalesce per type

When the daemon posts a system note, it SHALL first remove that task's existing unread system notes of the same `type`, so at most one unread note per type exists per task. Notes of different types and read notes SHALL be untouched. At startup the daemon SHALL idempotently collapse any existing backlog to the newest unread system note per task and type.

#### Scenario: New note replaces the previous one

- **WHEN** a second host-suspend note is posted to a task that already has an unread host-suspend note
- **THEN** only the newer host-suspend note remains unread for that task

#### Scenario: Different types coexist

- **WHEN** a bounce note is posted to a task with an unread host-suspend note
- **THEN** both remain

#### Scenario: Startup collapses the backlog

- **WHEN** the database holds many unread system notes of one type for a task at startup
- **THEN** only the newest remains, and running the collapse again changes nothing
