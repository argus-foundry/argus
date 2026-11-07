## ADDED Requirements

### Requirement: Live HEAD resolution

`gitutil.ReadHead` SHALL report the worktree's HEAD branch, commit oid and detached state, and `FetchGitStatus` SHALL include it in the snapshot. A failed read SHALL be an error, never a value derived from stored task state.

#### Scenario: Unborn
- **WHEN** a repo has no commits
- **THEN** the branch is the unborn branch name and the oid is null
