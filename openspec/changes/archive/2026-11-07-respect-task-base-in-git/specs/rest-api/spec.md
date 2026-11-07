## ADDED Requirements

### Requirement: Base-aware git status and diff

`GET /api/tasks/{id}/git/status` and `GET /api/tasks/{id}/git/diff?path=` SHALL compare against the task's `base_branch` when set and include a `comparison` object in the response. Existing fields keep their shapes. A git collection failure SHALL return 500 rather than an empty result.

#### Scenario: Comparison metadata
- **WHEN** a stacked task's status is requested
- **THEN** the response includes `comparison.requested_base` equal to the task's base

#### Scenario: Git failure
- **WHEN** the worktree is not a git repository
- **THEN** the response is 500
