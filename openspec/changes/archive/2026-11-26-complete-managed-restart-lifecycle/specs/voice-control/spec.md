## ADDED Requirements

### Requirement: Managed restarts report every terminal outcome

A successfully adopted managed replacement SHALL receive a fresh stream retry allowance. If a managed replacement exits before adoption, the daemon SHALL receive its confirmed exit and finalize that process's task lifecycle.

#### Scenario: A terminal follows several managed restarts

- **WHEN** a bound terminal follows more normal managed restarts than its transport retry count
- **THEN** it remains attached to the current process

#### Scenario: The replacement exits before its stream can attach

- **WHEN** a managed replacement starts and exits during the attach retry interval
- **THEN** the daemon applies that replacement's confirmed task exit

### Requirement: Scoped output has no replay gap

HTTP output SHALL join the same process's replay and live bytes without omitting output between replay and subscription.

#### Scenario: Output arrives during a replay

- **WHEN** the PTY publishes bytes while a process-scoped HTTP stream attaches
- **THEN** the client's replay and live stream collectively include the bytes in order

### Requirement: Reclaim stops the certified process only

Connected Hera reclaim SHALL require the owner to stop the certified process. Worktree removal SHALL wait until there is no active process or the owner accepts the stop for that process.

#### Scenario: The reclaim target changes before the stop

- **WHEN** the session owner reports a replacement rather than accepting the certified reclaim PID
- **THEN** the replacement and worktree remain available

#### Scenario: The reclaim exit has yet to be finalized

- **WHEN** a reclaim sweep sees an empty process slot while the task still has durable reclaim intent
- **THEN** it leaves the task and worktree available until the daemon settles the exit
