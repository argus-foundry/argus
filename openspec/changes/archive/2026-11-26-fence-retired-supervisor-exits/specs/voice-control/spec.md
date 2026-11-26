## ADDED Requirements

### Requirement: Relayed exits apply to the retired process

The daemon SHALL verify that a relayed supervisor exit belongs to the retiring process before changing task state and streams. Finalization of the retiring process and persistence of an explicit replacement SHALL be coordinated so the old exit cannot alter the replacement.

#### Scenario: A supervisor exit arrives after a replacement starts

- **WHEN** a delayed supervisor exit for an outgoing process reaches the daemon after a newer process for the task is active
- **THEN** the newer process remains active and keeps its task status and streams

#### Scenario: The replacement stream starts before the old stream finishes

- **WHEN** the client attaches a newly launched process before the earlier connection is cleaned up
- **THEN** it uses a fresh stream and output cursor, and cleanup of the retired connection preserves the new handle

### Requirement: Process-scoped width repair never affects a replacement

The owner SHALL verify the intended process before applying both resize and rerender operations requested with a process ID. Native repair SHALL refuse if the observed process is retiring or has been replaced.

#### Scenario: A replacement starts before a scoped width request reaches the owner

- **WHEN** the request's process ID no longer matches the live owner
- **THEN** the owner neither resizes nor restarts the replacement

### Requirement: Connected Hera reclaim preserves terminal completion

When the native TUI deliberately reclaims and archives a live Hera task, its terminal intent SHALL reach the daemon and apply to the confirmed exit of that session only.

#### Scenario: A connected operator reclaims a live task

- **WHEN** the task's reclaimed session exits
- **THEN** the archived task reaches Complete unless its active Hera worker binding requires review
