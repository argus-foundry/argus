## ADDED Requirements

### Requirement: Per-file line counts

The git status snapshot SHALL include one entry per changed path with `status`, `kind` (`text`, `binary`, `directory`, `unavailable`) and nonnegative `additions`/`deletions` for text only (null otherwise), computed with a bounded number of git commands independent of file count. Counts SHALL use the same local-first, merge-base-fallback scope as the single-file diff and the snapshot's comparison base. A count that cannot be computed SHALL be `unavailable`, never zero.

#### Scenario: Counts match file diff
- **WHEN** a repo has staged, unstaged, committed and untracked changes
- **THEN** each file's counts equal the added and removed lines of its single-file diff

#### Scenario: Binary and rename
- **WHEN** a binary file changes and a file is renamed
- **THEN** the binary has null counts and the rename carries `original_path`
