## Why

The workspace context popover can show total context fill but cannot explain which categories consume the Claude window. The bridge mod's `session.measure` carries only tokens, percent and window.

## What Changes

- The bridge mod reads the engine's `/context` breakdown (`$.session.usage({breakdown:'summary'})`) for released context measurements, in background single-flight work with a short deadline and reports optional per-category token counts (`messages`, `memory`, `tools`, `agents`, `skills`, `system`, `compaction`).
- `bridge.Context` gains optional category counters; the hub stores them with the rest of the measurement and clears them on session or launch change.
- Ingest drops invalid context measurements, logs them, and accepts the remaining batch events with a success response.
- New `GET /api/tasks/{id}/context` returns a backend-neutral `{task_id, session_id, provider, live, measured_at, source, context}` document. Unmeasured fields are `null`; a measured zero stays `0`.
- New `task.context_changed` event carries `{session_id, measured_at}` only, inviting a GET.
- Supervisor spawn surface 35 -> 36 (the mod ships in the supervisor binary).

## Non-Goals

- No web, TUI or macOS UI. Follow-ups: Web ENG-1077 (usage popover categories), TUI ENG-1092 (task details inspector), macOS `macos-context-inspector` (native task context categories UI). Codex fills the same response shape in ENG-1037.

## Impact

`internal/bridge`, `internal/bridgemod`, `internal/api`, `internal/model/event.go`, `internal/daemon/surface.go`.
