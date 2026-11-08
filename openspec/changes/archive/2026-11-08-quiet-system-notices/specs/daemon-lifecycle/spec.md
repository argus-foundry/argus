## ADDED Requirements

### Requirement: Daemon system notes coalesce

The host-suspend and bounce system notes SHALL each be posted through the coalescing system-note path, so a task never accumulates more than one unread note of each type, and neither note counts as unread mail or toward the inbox cap.

#### Scenario: Repeated suspends leave one note

- **WHEN** the host suspends and resumes twice while a task is running
- **THEN** the task holds exactly one unread host-suspend note, from the latest suspend
