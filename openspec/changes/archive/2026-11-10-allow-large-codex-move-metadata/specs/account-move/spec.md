## MODIFIED Requirements

### Requirement: A task can be moved to another account

Move validation SHALL allow up to 4 MiB for the first JSONL session_meta record, including its newline. Native base_instructions and dynamic_tools fields may exceed 64 KiB. A record beyond the move-specific ceiling SHALL be refused before destination publication. Existing context-meter readers SHALL retain their 64 KiB default. Thread identity, Nlink, same-descriptor validation, rewind and complete copying remain required.

#### Scenario: Large native instructions
- **WHEN** matching session metadata contains base_instructions.text larger than 64 KiB but fits the move budget
- **THEN** the move succeeds and copies the entire header and following records byte-for-byte

#### Scenario: Move metadata exceeds the resource budget
- **WHEN** the first JSONL record exceeds 4 MiB
- **THEN** the move is refused and task, files and indexes remain unchanged
