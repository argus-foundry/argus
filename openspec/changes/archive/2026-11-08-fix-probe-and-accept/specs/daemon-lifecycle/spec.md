## ADDED Requirements

### Requirement: Session status never reports a live supervisor session dead from a detached handle

In supervisor mode, when the daemon's cached handle for a session reads dead, the daemon SHALL ask the supervisor before answering a status probe: if the supervisor reports the session alive it SHALL report that; if the supervisor cannot be asked it SHALL answer an error rather than a verdict; only a supervisor-confirmed absence SHALL be reported as not alive. A daemon that is shutting down and has detached its supervisor client SHALL therefore never publish a death verdict for a session the supervisor still runs.

#### Scenario: Detached daemon does not report a live session dead
- **WHEN** the old daemon has detached its supervisor client during a restart and a client asks it for the status of a session the supervisor still runs
- **THEN** the answer is an error (unknown) or alive, never not-alive

#### Scenario: Genuine exit is still reported
- **WHEN** the supervisor no longer has the session
- **THEN** the status probe reports not alive with no error
