## ADDED Requirements

### Requirement: Paged read-only task history

The store SHALL provide a cursor-paged, id-ordered, read-only history of messages addressed to a task's roles, including ended bindings.

#### Scenario: Stable paging with identical timestamps

- **WHEN** messages share a `sent_at`
- **THEN** pages are ordered and cursored by id without gaps or repeats
