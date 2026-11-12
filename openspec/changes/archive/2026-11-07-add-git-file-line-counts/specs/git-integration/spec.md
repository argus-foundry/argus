## ADDED Requirements

### Requirement: Per-file line counts

The git status snapshot SHALL include one entry per changed path with `status`, `kind` (`text`, `binary`, `directory`, `unavailable`) and nonnegative `additions`/`deletions` for text only (null otherwise), computed with a bounded number of git commands independent of file count. Counts SHALL use the same local-first, merge-base-fallback scope as the single-file diff and the snapshot's comparison base. A count that cannot reliably match the single-file diff SHALL be `unavailable`, never zero. Unmerged paths and paths requiring text conversion SHALL have null counts. Untracked reads SHALL be confined to the worktree, nonblocking, and bounded to 8 MiB of text; forced binary attributes SHALL produce `binary`, while unsupported conversions SHALL produce `unavailable`.

#### Scenario: Counts match file diff
- **WHEN** a repo has staged, unstaged, committed and untracked changes
- **THEN** each available text file's counts equal the added and removed lines of its single-file diff, with local changes taking priority on paths also changed in the branch

#### Scenario: Binary and rename
- **WHEN** a binary file changes and a file is renamed
- **THEN** the binary has null counts and the rename carries `original_path`, with counts matching the destination-only single-file diff rather than rename similarity
