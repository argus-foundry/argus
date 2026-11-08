## Why

Codex sessions have no context producer, so the workspace ring cannot show how close a Codex conversation is to its window. Codex runs no bridge mod, but it records a `token_count` event with the last call's usage and `model_context_window` in its own rollout file.

## What Changes

- New `internal/codexctx`: a bounded (1 MiB tail), read-only rollout reader (`Latest`, `LatestInFile`) plus a thread-index lookup scoped to worktree and recorded session (`FindThread`). Reusable by the session/turn token work.
- `agent.Family` gains `ContextReader`, `ContextProvider`, `ContextSource`; the Codex family fills them (`openai`, `codex_rollout`). Callers read the registry, never the backend name.
- `GET /api/tasks/{id}/context` serves reader-backed families through the same response shape; `categories` stay null.
- The idle-watcher tick emits `task.context_changed` `{session_id, measured_at}` when a running reader-backed task's measurement time advances.

## Non-Goals

- No web, TUI or macOS UI. Follow-ups: Web ENG-1078 (composer context ring), TUI ENG-1092 (task details inspector).
- No categories for Codex (the rollout carries none); no cumulative or quota figures.

## Impact

`internal/codexctx`, `internal/agent`, `internal/api`.
