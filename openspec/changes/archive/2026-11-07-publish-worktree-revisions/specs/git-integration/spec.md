## ADDED Requirements

### Requirement: Worktree revision cache

The daemon SHALL maintain, per task worktree, a cached git status and an opaque revision derived from the status message and its comparison. Filesystem changes in the worktree and HEAD/index changes in its git directory SHALL mark the cache dirty, and bursts SHALL coalesce into a single-flight refresh. The cache SHALL be keyed on the task's comparison base and SHALL be discarded when the task is deleted or archived or its worktree disappears.

#### Scenario: Edit changes the revision
- **WHEN** a watched worktree file is edited
- **THEN** a `worktree.changed` event with a new revision is published

#### Scenario: Burst coalesces
- **WHEN** many writes land within the debounce window
- **THEN** at most one refresh runs per window and an unchanged resulting status publishes nothing
