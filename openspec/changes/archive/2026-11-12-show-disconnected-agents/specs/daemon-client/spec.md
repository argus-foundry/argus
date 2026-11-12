## ADDED Requirements

### Requirement: Stream reconnect survives a daemon restart

When a session output stream drops while the daemon still reports the process alive, the client SHALL retry with bounded exponential backoff totalling about 15 seconds (not about 1.5s), log a uxlog line on reconnect success and on give-up, and on exhaustion treat the session as stream-lost (never as a confirmed exit). The client SHALL expose whether its last session-list read succeeded so callers can fail open.

#### Scenario: Daemon back within the window

- **WHEN** the stream drops and the daemon accepts a stream again within the backoff window
- **THEN** the stream reconnects and a success line is logged

#### Scenario: Daemon never returns

- **WHEN** every retry fails
- **THEN** a give-up line is logged and the session is removed as stream-lost
