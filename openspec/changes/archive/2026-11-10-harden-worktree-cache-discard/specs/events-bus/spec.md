## MODIFIED Requirements

### Requirement: Discard publishes a worktree change

After a successful discard the daemon SHALL emit `worktree.changed` with reason `discard` and the discarded path, advertising a revision only after a completed successful post-mutation refresh. On refresh timeout or collection failure the cache SHALL remain stale and the event SHALL omit the revision so consumers refetch.

#### Scenario: Failure emits nothing
- **WHEN** a discard is rejected
- **THEN** no `worktree.changed` event with reason `discard` is emitted
#### Scenario: Refresh timeout
- **WHEN** the post-discard refresh exceeds the bound while an older cached snapshot exists
- **THEN** the discard event has no revision and the cached snapshot remains stale until refreshed
