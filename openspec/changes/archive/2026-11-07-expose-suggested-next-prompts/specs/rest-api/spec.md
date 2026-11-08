## ADDED Requirements

### Requirement: Bridge state carries the suggested next prompt
`GET /api/tasks/{id}/bridge` SHALL include `suggestion`, either `null` or `{text, session_id, generation, recorded_at}`. It SHALL be `null` when the provider offered none, the provider has no suggestion support, the suggestion was cleared or replaced, or the bridge is not live authority for the running launch. The existing 404 without bridge state SHALL be preserved. When the stored suggestion appears, changes or clears the daemon SHALL emit `task.suggestion_changed` with `{session_id, available, recorded_at}` and no text.

#### Scenario: Current suggestion
- **WHEN** the current launch reports a suggestion
- **THEN** GET returns it with session identity and timestamp and one `task.suggestion_changed` event with `available=true` was emitted

#### Scenario: Lost authority
- **WHEN** the running process's generation differs from the recorded one
- **THEN** GET returns `suggestion=null` with `live=false`
