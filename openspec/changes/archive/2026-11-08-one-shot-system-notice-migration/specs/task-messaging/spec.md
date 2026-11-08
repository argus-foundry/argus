## MODIFIED Requirements

### Requirement: System notes coalesce per type

When the daemon posts a system note, it SHALL first remove that task's existing unread system notes of the same `type`, so at most one unread note per type exists per task. Notes of different types and read notes SHALL be untouched. Exactly once per database, at the first startup after upgrade, the daemon SHALL collapse any existing backlog to the newest unread system note per task and type and mark the surviving legacy notes read; the migration SHALL record completion only after it succeeds (so a failure retries) and SHALL NOT run again, so notes posted after the migration stay unread.

#### Scenario: New note replaces the previous one

- **WHEN** a second host-suspend note is posted to a task that already has an unread host-suspend note
- **THEN** only the newer host-suspend note remains unread for that task

#### Scenario: Different types coexist

- **WHEN** a bounce note is posted to a task with an unread host-suspend note
- **THEN** both remain

#### Scenario: One-shot migration collapses and marks the backlog read

- **WHEN** the database holds many unread legacy system notes of one type for a task at the first startup after upgrade
- **THEN** only the newest remains and it is marked read

#### Scenario: Later startups leave new notes unread

- **WHEN** a system note is posted after the migration and the daemon restarts
- **THEN** that note is still unread

#### Scenario: Failed migration retries

- **WHEN** the migration fails before completing
- **THEN** it records no completion and runs again at the next startup
