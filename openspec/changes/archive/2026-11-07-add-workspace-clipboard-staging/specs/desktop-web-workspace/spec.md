## ADDED Requirements

### Requirement: Selected-task staged clipboard
The workspace SHALL show a compact, keyboard-accessible Copy staged text button beside the selected task's terminal only when staging is present, including an empty staged string. Its shared tooltip SHALL show a bounded plaintext preview. Selection SHALL read the existing clipboard endpoint, treating 204 as absent; task output clipboard SSE frames SHALL update or clear staging without changing the terminal output cursor. Reconnecting the output stream SHALL refresh staging. Late reads and events from other selections SHALL NOT replace current state; a live event SHALL supersede an earlier pending read. Malformed frames and staging read failures SHALL be logged without breaking terminal output. Clipboard reads and event logs SHALL NOT include staged text.

#### Scenario: Load and copy repeatedly
- **WHEN** a selected task has staged text
- **THEN** its copy button and preview SHALL appear
- **AND** mouse or keyboard activation SHALL use shared clipboard success/failure feedback
- **AND** repeated copies SHALL preserve staging without a DELETE request
- **AND** Cmd+Shift+C SHALL retain its separate visible-output copy behavior

#### Scenario: Live replacement and clearing
- **WHEN** the selected task receives a clipboard frame with text, including an empty string
- **THEN** the control SHALL copy that exact text
- **WHEN** a cleared frame or a 204 read reports absence
- **THEN** the control SHALL hide and restore composer focus only if it owned focus

#### Scenario: Selection and stream recovery
- **WHEN** selection changes or the task stream reconnects
- **THEN** staging SHALL be read for the selected task
- **AND** late reads, superseded stream events and another task's frames SHALL NOT overwrite selected-task staging

#### Scenario: Staging failure isolation
- **WHEN** a clipboard frame is malformed, a staging read fails or a clipboard write is denied
- **THEN** terminal output SHALL remain usable, failures SHALL be logged without payload text, and copy failures SHALL use shared feedback
