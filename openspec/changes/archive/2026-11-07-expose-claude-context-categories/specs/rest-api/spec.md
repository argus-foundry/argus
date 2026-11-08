## ADDED Requirements

### Requirement: Task context endpoint
`GET /api/tasks/{id}/context` SHALL return `{task_id, session_id, provider, live, measured_at, source, context}` where `context` is `{used_tokens, window_tokens, percent, categories}` and `categories` always lists `messages`, `memory`, `tools`, `agents`, `skills`, `system`, `compaction` with `null` for unmeasured values. It SHALL return 404 for a missing task and 200 with `live=false` and `context=null` for a task without a measurement or whose recorded measurement belongs to a replaced launch. The `live` flag SHALL reflect current process authority and bridge health, not measurement age. Unchanged context is deduplicated, so an old measurement with fresh heartbeats from the current launch SHALL remain `live=true`. Clients MAY use `measured_at` to display age. Replacing the session, process generation or account launch SHALL invalidate the old context. When the bridge is no longer live without a known replacement, recorded data SHALL return with `live=false`. It SHALL require authentication and expose aggregate counts only. After a live measurement the daemon SHALL emit `task.context_changed` with `{session_id, measured_at}` and no counts.

#### Scenario: Live measurement
- **WHEN** the current launch reports a measurement
- **THEN** GET returns `live=true` with its counts and one `task.context_changed` event was emitted

#### Scenario: Replaced session
- **WHEN** the running process's generation differs from the recorded measurement's
- **THEN** GET returns `live=false` and `context=null`

#### Scenario: Unchanged context remains live with fresh heartbeats
- **WHEN** a ten-minute-old context measurement belongs to the current launch and a fresh heartbeat arrives
- **THEN** GET returns `live=true` with the original counts and original `measured_at`

#### Scenario: Account replacement invalidates old context
- **WHEN** a task moves from account A to account B with a replacement process generation
- **THEN** GET hides A's context until B reports its own measurement, then returns B's session identity and category counts
