## ADDED Requirements

### Requirement: Git status file counts

`GET /api/tasks/{id}/git/status` SHALL include `files` from the same snapshot and comparison base as the legacy `Status`, `Diff`, `BranchDiff` and `BranchFiles` fields, which remain populated. Cold status collection failures return 500; a failed refresh with a cached snapshot returns the prior snapshot with 200 and `stale=true`. Cold reads that exhaust the bounded wait return 503.

#### Scenario: Files alongside legacy fields
- **WHEN** a worktree has a modified file
- **THEN** the response has `Status` text and a `files` entry with its counts
