## ADDED Requirements

### Requirement: Task context endpoint
`GET /api/tasks/{id}/context` SHALL return `{task_id, session_id, provider, live, measured_at, source, context}` where `context` is `{used_tokens, window_tokens, percent, categories}` and `categories` always lists `messages`, `memory`, `tools`, `agents`, `skills`, `system`, `compaction` with `null` for unmeasured values. It SHALL return 404 for a missing task and 200 with `live=false` and `context=null` for a task without a measurement or whose recorded measurement belongs to a replaced launch. A stale recorded measurement SHALL return `live=false` with its data. It SHALL require authentication and expose aggregate counts only. After a live measurement the daemon SHALL emit `task.context_changed` with `{session_id, measured_at}` and no counts.

#### Scenario: Live measurement
- **WHEN** the current launch reports a measurement
- **THEN** GET returns `live=true` with its counts and one `task.context_changed` event was emitted

#### Scenario: Replaced session
- **WHEN** the running process's generation differs from the recorded measurement's
- **THEN** GET returns `live=false` and `context=null`
