## MODIFIED Requirements

### Requirement: Task context endpoint
`GET /api/tasks/{id}/context` SHALL return `{task_id, session_id, provider, live, measured_at, source, context}` where `context` is `{used_tokens, window_tokens, percent, categories}` and `categories` always lists `messages`, `memory`, `tools`, `agents`, `skills`, `system`, `compaction` with `null` for unmeasured values. It SHALL return 404 for a missing task and 200 with `live=false` and `context=null` for a task without a measurement or whose recorded measurement belongs to a replaced launch or session. The `live` flag SHALL reflect current launch authority rather than measurement age. Recorded data without a running session SHALL return `live=false`. It SHALL require authentication and expose aggregate counts only. After a live measurement the daemon SHALL emit `task.context_changed` with `{session_id, measured_at}` and no counts. For a backend family that declares a context reader (Codex: `provider` `openai`, `source` `codex_rollout`), the measurement SHALL come from the task's own recorded session on disk, SHALL use the latest `last_token_usage.total_tokens`, including compaction estimates, rather than cumulative tokens, SHALL leave `window_tokens` and `percent` null when the window is unknown, and SHALL NOT launch a CLI or model turn. The daemon SHALL emit `task.context_changed` for such a task when its measurement time advances while it runs.

#### Scenario: Live measurement
- **WHEN** the current launch reports a measurement
- **THEN** GET returns `live=true` with its counts and one `task.context_changed` event was emitted

#### Scenario: Replaced session
- **WHEN** the running process's generation differs from the recorded measurement's
- **THEN** GET returns `live=false` and `context=null`

#### Scenario: Codex measured context
- **WHEN** a Codex task's recorded rollout holds a token_count event with a window
- **THEN** GET returns `provider=openai`, `source=codex_rollout`, `used_tokens` from the last call, the window and a derived percent

#### Scenario: Codex session replaced or unmeasured
- **WHEN** the task's recorded session id has no thread in its worktree, or the rollout has no usable event
- **THEN** GET returns 200 with `context=null`
