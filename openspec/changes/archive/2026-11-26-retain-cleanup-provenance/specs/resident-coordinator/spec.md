## ADDED Requirements

### Requirement: Keep the captured cleanup repository

Deferred resident cleanup SHALL retain the repository captured at nuke time, including for a worktree with no branch. TUI stack confirmation SHALL carry the Git repository used for merge evidence into deletion.

#### Scenario: The path contains a new repository checkout
- **WHEN** a managed worktree path is registered to another repository after reclaim was recorded
- **THEN** the sweep preserves that checkout and keeps the original repository identity.

#### Scenario: Repository configuration changes during confirmation
- **WHEN** the user confirms a stacked branch cleanup after the project points to another repository
- **THEN** the previous repository's evidence does not authorize deletion in the new repository.
