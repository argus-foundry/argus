## ADDED Requirements

### Requirement: Task-recycle seed carries the last exchange and is built at restart time
The `task_recycle` seed prompt SHALL be composed after the idle wait, at restart time, and SHALL include the daemon-extracted "Last exchange with the user before the reset" section and the relay directive, with the same fail-open, size-capped, local-only rules as the hera recycle seed. The `task_recycle` tool and skill SHALL require the handoff_note to include an "Open with the user" section.

#### Scenario: Final reply is captured
- **WHEN** the session writes its final reply to the user after calling task_recycle and then goes idle
- **THEN** the seed built at restart includes that reply in the last-exchange section
