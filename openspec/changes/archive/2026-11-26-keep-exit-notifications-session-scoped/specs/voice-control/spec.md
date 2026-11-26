## ADDED Requirements

### Requirement: Exit notifications retain session ownership

A daemon-backed TUI SHALL leave persisted task status and session identity to the daemon. A notification from a retired session SHALL NOT detach a live replacement or alter its task state. The standalone TUI SHALL keep persisting its own session exits.

#### Scenario: An outgoing exit notification arrives after a resume

- **WHEN** a daemon-backed TUI receives a delayed notification for a previously stopped session after a replacement has started
- **THEN** the replacement stays connected and its persisted task remains in progress

### Requirement: Owner-managed remote TUI rerender preserves width decisions

When the remote TUI requests a rerender, the daemon SHALL consider the committed width for the current process and allow a deliberate same-width retry. The daemon SHALL still verify the process, transcript availability, and the normal safety conditions before queuing a restart.

#### Scenario: A panel returns to the initial width after repair was deferred

- **WHEN** the panel returns to the process's initial width but differs sufficiently from the committed history width for that process
- **THEN** the daemon evaluates a rerender at the panel width

#### Scenario: An explicit rerender uses a previously requested width

- **WHEN** the user requests a rerender at a width already sent to the daemon
- **THEN** the daemon evaluates the request again using the current process and the normal safety checks

### Requirement: Resume waits for stopped sessions to retire

A resume SHALL refuse to reattach to a session that has already received a stop. Native TUI repair SHALL refuse to schedule a restart against that process, including for resident tasks whose status stays in progress. The same task MAY be resumed once the session owner retires the stopped process.

#### Scenario: A task is resumed while its process is terminating

- **WHEN** the session owner is still retiring a previously stopped process
- **THEN** REST and voice report a restart conflict and the native TUI leaves the stopped state intact
