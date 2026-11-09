## ADDED Requirements

### Requirement: Band unread excludes system notes

The daemon's bridge UI snapshot SHALL NOT include daemon-originated system notes in its unread count or message list, so the band and `/hera-inbox` never report "unread from argus:system".

#### Scenario: Only system notes pending

- **WHEN** a task's unread mail consists only of system notes
- **THEN** the snapshot reports `unread` 0 and an empty message list
