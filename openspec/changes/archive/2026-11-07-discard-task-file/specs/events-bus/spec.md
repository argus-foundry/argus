## ADDED Requirements

### Requirement: Discard publishes a worktree change

After a successful discard the daemon SHALL emit `worktree.changed` with reason `discard` and the discarded path, only once the status cache reflects the new file state.

#### Scenario: Failure emits nothing
- **WHEN** a discard is rejected
- **THEN** no `worktree.changed` event with reason `discard` is emitted
