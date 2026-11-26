## ADDED Requirements

### Requirement: Launch finalization preserves a concurrent stop

Successful REST and voice stops SHALL supersede overlapping launch finalization, including when the task was already in review. REST stop SHALL return a conflict for a Pending task without a session and preserve its status. The TUI SHALL attach to an existing session after create or fork and leave a concurrent stop in effect. Launch persistence SHALL acquire SQLite write intent before reading the state that it will update.

#### Scenario: REST stop before the PTY exists

- **WHEN** REST stop targets a Pending task before its first session is available
- **THEN** the call returns a conflict and the task stays Pending so normal launch finalization can proceed

#### Scenario: Stop supersedes native resume from in review

- **WHEN** a voice stop terminates the PTY while the native TUI is completing a resume
- **THEN** launch finalization preserves the stopped task state even when it was already in review before resume
