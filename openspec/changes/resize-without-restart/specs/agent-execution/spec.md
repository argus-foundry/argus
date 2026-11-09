## ADDED Requirements

### Requirement: Restarted sessions spawn at the reported pane size

A recycle or kick-driven restart SHALL start the new session at the size resolved in this order: the fresh viewer-reported pane size for the task's role (`db.InitialPTYSize`), then the outgoing session's live PTY size when non-zero, then 24x80. The resolution SHALL happen before the supervisor RPC using the existing `Rows`/`Cols` fields, SHALL NOT change the supervisor protocol version, and SHALL be logged with its source.

#### Scenario: Fresh report wins over a stale PTY

- **WHEN** a coordinator is recycled whose PTY was last sized 80x24 but a fresh coordinator-pane report says 150x45
- **THEN** the new session starts at 150x45 and the log names source `report`

#### Scenario: No report falls back to the live PTY

- **WHEN** no fresh report exists and the outgoing PTY is 120x40
- **THEN** the new session starts at 120x40 with source `pty`

#### Scenario: No session and no report

- **WHEN** the outgoing session is gone and no report exists
- **THEN** the new session starts at 24x80 with source `default`
