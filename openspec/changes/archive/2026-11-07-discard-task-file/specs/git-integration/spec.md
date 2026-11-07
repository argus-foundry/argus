## ADDED Requirements

### Requirement: Single-file discard

The daemon SHALL restore one worktree path to HEAD, undoing staged and unstaged edits, without touching other paths. Paths not in HEAD SHALL be removed only on explicit request. Rename pairs and unmerged paths SHALL be refused, and containment SHALL be re-verified immediately before mutation.

#### Scenario: Tracked edits restored
- **WHEN** a tracked file has staged and unstaged edits and is discarded
- **THEN** it matches HEAD and other files are unchanged

#### Scenario: Escape rejected
- **WHEN** the path traverses a symlink leaving the worktree
- **THEN** it is rejected and the outside file is untouched
