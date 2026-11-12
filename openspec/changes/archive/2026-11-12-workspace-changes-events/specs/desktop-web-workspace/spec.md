## ADDED Requirements

### Requirement: Live Changes snapshots
The workspace SHALL consume matching worktree.changed events directly even during task snapshot buffering, deduplicate opaque revisions, fence snapshots invalidated in flight, surface stale snapshots and retry them with bounded backoff, and converge on reconnect/resync. Hidden panes SHALL defer reads until shown. Changes SHALL have no discovery interval; Artifacts retains its existing interval.

#### Scenario: Adopt the merged contract
- **WHEN** An external edit sends a revision or revisionless invalidation; only the visible matching task refreshes, and an earlier snapshot cannot overwrite it.
- **THEN** the client follows the merged daemon contract, retaining captured-task cancellation and accessible feedback
