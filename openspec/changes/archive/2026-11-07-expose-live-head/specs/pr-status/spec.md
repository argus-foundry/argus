## ADDED Requirements

### Requirement: Automatic PR lookup follows the live branch

The automatic PR poller SHALL query the branch currently checked out in the task's worktree, not `Task.Branch`. A task whose HEAD is detached or unreadable SHALL be skipped with its cached PR state kept. Manually linked tasks remain skipped.

#### Scenario: Checkout after creation
- **WHEN** a task created on branch A has branch B checked out
- **THEN** the poller queries branch B and A is not queried
