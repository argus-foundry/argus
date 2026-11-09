## ADDED Requirements

### Requirement: Task usage endpoint
`GET /api/tasks/{id}/usage` SHALL return `{task_id, session_id, provider, measured_at, source, totals, turns, next_cursor}` where `totals` is `{input_tokens, cached_input_tokens, output_tokens}`, each turn is `{turn_id, input_tokens, cached_input_tokens, output_tokens}` listed newest first, and `input_tokens` includes `cached_input_tokens`. Counters SHALL be `null` when unreported and `0` only when measured zero. Omitting `session_id` SHALL select the task's current session even before its first measurement, returning that session ID with null totals and empty turns. A requested session not recorded for the task SHALL be 404, including for unsupported backends. `limit` defaults to 50 and is capped at 200; a non-positive or non-numeric `limit` or an invalid `before` cursor SHALL be 400. A task whose backend has no usage telemetry, or with no measurement yet, SHALL return 200 with `totals=null` and `turns=[]`. The endpoint SHALL require authentication, expose counts only, and never launch a CLI or model call. A reader failure SHALL NOT replace stored counters, and an older observation SHALL NOT overwrite a newer one. Sessions SHALL never be combined. Usage SHALL reuse the context reader's account storage, rollout metadata identity and uncaptured process launch checks. Identical observations SHALL make no row changes and turns outside retention SHALL NOT be reinserted. For a running task the daemon SHALL emit `task.usage_changed` with `{session_id, turn_id, measured_at}` and no counts after the stored observation advances, whether GET or the watcher ingests first. A new retained turn SHALL count as a change even when the session measurement timestamp is unchanged.

#### Scenario: Codex session and turns
- **WHEN** a Codex task's rollout holds two turns with token_count events
- **THEN** GET returns cumulative totals, both turns newest first, and re-reading changes nothing

#### Scenario: Replaced session
- **WHEN** the task's recorded session changes
- **THEN** the default GET reflects the new session only, with null totals and empty turns until measured, and `session_id=<old>` still returns the old session's own totals

#### Scenario: Unsupported provider
- **WHEN** the task's backend has no usage reader
- **THEN** GET returns 200 with `totals=null` and `turns=[]`

#### Scenario: GET ingests before watcher
- **WHEN** a GET ingests a changed observation before the next watcher tick
- **THEN** one usage change event is published and the unchanged watcher read does not duplicate it

#### Scenario: Uncaptured replacement launch
- **WHEN** a fresh live process has no captured session ID and only an older conversation is readable or stored
- **THEN** default GET returns null totals and empty turns without ingesting the older conversation

#### Scenario: Unpinned current session before measurement
- **WHEN** an unpinned task resolves and validates a newer main thread without a token measurement
- **THEN** default GET returns that thread ID with null totals and empty turns, whether stopped or live, without creating a measured usage row
