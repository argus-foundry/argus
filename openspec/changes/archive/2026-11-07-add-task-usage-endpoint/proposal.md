## Why

Users cannot inspect token usage for one task session or turn. Hera role totals sum every binding and cannot explain current session activity. Codex records cumulative and per-call usage plus turn boundaries in its rollout file, which `internal/codexctx` already reads for context fill.

## What Changes

- `codexctx.Usage` / `UsageInFile`: bounded (16 MiB tail), read-only parse of session totals (cumulative `total_token_usage`) and per-turn spend (sum of each call's `last_token_usage`, repeated identical `token_count` lines counted once, turns that began before the window dropped).
- `agent.Family` gains `UsageReader`, `UsageProvider`, `UsageSource`; Codex fills them. The parse is cached per rollout on size and mtime so the per-tick poll stays cheap.
- New tables `task_usage_sessions` and `task_usage_turns`, keyed by (task, session[, turn]), created with `CREATE TABLE IF NOT EXISTS`. Upserts are idempotent and refuse older observations. Retention: newest 500 turns per session, newest 8 sessions per task; `DB.Delete` removes a task's rows.
- `GET /api/tasks/{id}/usage?session_id=&limit=&before=` serves totals and newest-first turns from the store, refreshed from the reader on each request.
- The idle-watcher tick emits `task.usage_changed` `{session_id, turn_id, measured_at}` when a running reader-backed task's stored observation advances.

## Non-Goals

- No web, TUI or macOS UI. Follow-ups: Web ENG-1080 (session and turn tokens in task usage), TUI ENG-1092, macOS `macos-task-usage-consumption` (task details inspector).
- No Claude usage: the bridge mod reports no per-turn token counters, so Claude tasks answer unavailable (totals null, turns empty). Adding counters to the mod is a separate change.
- `db/hera_cost.go` accrual is unchanged; usage here is never summed from Hera bindings.

## Impact

`internal/codexctx`, `internal/agent`, `internal/db`, `internal/api`, `internal/model`.
