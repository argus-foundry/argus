## MODIFIED Requirements

### Requirement: Cached exit info on session end

When a session exits, the daemon SHALL cache its exit information (error string, stopped flag, last output, pending-restart flag) so a client can query it once after the stream closes. The cached info SHALL be consumed on read. A query for which no exit is recorded (the session never exited, the record expired, or it was already consumed) SHALL answer a stream-lost result, never an empty record, because an empty record reads as a clean exit and would complete a live task. Stream-lost leaves task status unchanged.

#### Scenario: Consume-once exit info
- **WHEN** a client queries exit info for a finished session
- **THEN** the cached info is returned and removed, and a subsequent query returns stream-lost

#### Scenario: No recorded exit is not a clean exit
- **WHEN** a client queries exit info for a session with no recorded exit, such as after a post-restart status probe wrongly reported it dead
- **THEN** the result is stream-lost and the task's status is not changed to complete

#### Scenario: Old server's empty record is not a clean exit
- **WHEN** the daemon-side client receives an exit-info reply with no error, no stop, no pending restart and no captured output from a server that has not been restarted
- **THEN** it treats the exit as unconfirmed (stream-lost) and the task's status is not changed to complete; an exit that carries output is still a clean exit

#### Scenario: Failed supervisor probe is not a death verdict
- **WHEN** the daemon cannot query the supervisor about a session
- **THEN** the status probe fails with an error rather than reporting the session not alive
