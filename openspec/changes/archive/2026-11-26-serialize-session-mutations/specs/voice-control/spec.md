## ADDED Requirements

### Requirement: Concurrent session lifecycle operations

Native TUI and daemon stop and resume operations for a task SHALL be mutually exclusive through persisted finalization. REST stop SHALL refuse while the session owner has reserved a start or queued a restart. A deferred native restart SHALL be discarded when a confirmed stop has advanced its recorded stop generation.

#### Scenario: An outgoing session stops while the user resumes the task

- **WHEN** a native TUI resume is attempted while the daemon has signaled the outgoing session and is still persisting the stop
- **THEN** the TUI leaves the task alone and the user can resume after the stop operation completes

#### Scenario: A scheduled native restart overlaps a confirmed stop

- **WHEN** a successful voice or REST stop advances the stop generation before a queued native rerender resumes
- **THEN** the deferred rerender leaves the task stopped

#### Scenario: Another resume already launched the task

- **WHEN** the native TUI attempts a resume after the daemon has launched a session for the same task
- **THEN** the TUI leaves the existing session and its persisted state intact
