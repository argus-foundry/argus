## ADDED Requirements

### Requirement: Task list shows the disconnected glyph

The Tasks-tab status column SHALL render the same dim `○` for an in_progress or in_review task whose session is absent from the daemon alive set for 2 consecutive ticks (fail-open when the list is unreadable), via the shared `TaskStatusIcon` classifier. Needs-input SHALL outrank it.

#### Scenario: In-progress task with a dead session

- **WHEN** an in_progress task has no alive session for 2 ticks
- **THEN** its row shows `○` in the dimmed style instead of the idle moon
