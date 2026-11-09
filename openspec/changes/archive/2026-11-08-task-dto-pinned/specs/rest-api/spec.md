## ADDED Requirements

### Requirement: Stored pin state in task snapshots

Every task returned by `GET /api/tasks` and `GET /api/tasks/{id}` SHALL include a boolean `pinned` equal to the stored task's pin state. The field SHALL always be emitted, including false, so clients can distinguish an unpinned task from an older daemon that omits the field. Existing filters, routes and `task.updated` invalidation behavior SHALL remain unchanged.

#### Scenario: Pinned and unpinned task snapshots

- **WHEN** either task GET returns a pinned or unpinned task
- **THEN** its response explicitly contains `pinned:true` or `pinned:false` matching storage

#### Scenario: Snapshots after pin lifecycle mutations

- **WHEN** a task is pinned, unpinned, archived or restored by pinning
- **THEN** both task GETs reflect its stored pin state
- **AND** archiving clears the pin while pinning clears archived
