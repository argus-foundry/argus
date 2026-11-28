## ADDED Requirements

### Requirement: Outgoing identity through terminal handoff

A queued managed restart SHALL report the outgoing process identity before the process stops. Recorded terminal history SHALL refresh from the current snapshot when the producing process cannot be identified.

#### Scenario: Daemon joins before outgoing exit receipt
- **WHEN** a daemon observes a queued restart while the outgoing process is retired and its exit callback has yet to publish a receipt
- **THEN** the observation identifies the outgoing PID and the relay waits for its scoped receipt.

#### Scenario: Recorded history changes while Theia is suspended
- **WHEN** Theia resumes a cached terminal and the new snapshot contains recorded output with no process ID
- **THEN** it replaces the displayed history with that snapshot without using its byte count as a live stream cursor.

#### Scenario: Relaunch races a Hera reclaim stop
- **WHEN** an archived Hera task still has an armed stop for its retiring process
- **THEN** restart and resume are refused until that stop settles, regardless of the task's workflow status.

#### Scenario: Nuke is requested through REST or standalone TUI
- **WHEN** the final role is nuked
- **THEN** binding admission is fenced through the scoped stop and the worktree is removed only after owner retirement.

#### Scenario: An SSE client sees a pending relay
- **WHEN** an attachment sees an owner relay without a PID or a remote terminal receives a stale snapshot conflict during replacement
- **THEN** it cannot consume output with an unscoped cursor, and the terminal waits for settled owner state before reconnecting or declaring exit.
