## ADDED Requirements

### Requirement: Revisioned git status

`GET /api/tasks/{id}/git/status` SHALL include `git_revision` and `stale`. A cold read SHALL await one bounded refresh and return 503 `{"error","code":"git_status_unavailable"}` on timeout. Existing 404, 500 and authentication behavior is unchanged.

#### Scenario: Cached read
- **WHEN** status is requested twice with no change
- **THEN** both responses carry the same `git_revision` and `stale=false`

#### Scenario: Cold timeout
- **WHEN** the first refresh exceeds the bound
- **THEN** the response is 503 with code `git_status_unavailable`
