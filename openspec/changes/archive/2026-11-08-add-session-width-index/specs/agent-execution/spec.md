## ADDED Requirements

### Requirement: Session log width index

A session SHALL record its PTY size history in `<sessions>/<task>.sizes`, one `<logOffset> <cols> <rows>` line per size, where logOffset is the number of bytes written to the session log when the size took effect. The index SHALL be truncated and seeded with the starting size at offset 0 whenever the log is truncated for a new session, SHALL gain a line only when the size actually changes, and SHALL be removed with the task's other session files. A reader SHALL ignore malformed lines, a torn final line, and offsets that decrease.

#### Scenario: Resize is recorded at the log offset
- **WHEN** the PTY is resized after N bytes have been logged
- **THEN** a line with offset N and the new size is appended, and repeating the same size appends nothing

#### Scenario: New session resets the index
- **WHEN** a session starts and truncates its log
- **THEN** the index contains only the starting size at offset 0

#### Scenario: Torn index
- **WHEN** the last line is incomplete or an offset goes backwards
- **THEN** those lines are ignored and earlier marks still apply
