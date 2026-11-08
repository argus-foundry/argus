## Why

The workspace composer cannot show the prompt the provider itself suggests for the next turn. The Claude engine exposes an authoritative `prompt.suggest` hook (its own post-turn guess), so no terminal scraping is needed.

## What Changes

- The bridge mod hooks `prompt.suggest`, and reports `suggestion.changed {text}` only for the engine's own proposal (`origin.kind === 'suggestion'`) that the prompt box will show. Plugin-origin proposals and hidden ones are never reported. Text over 4 KiB, blank or NUL-bearing text is not sent.
- The daemon accepts `suggestion.changed` / `suggestion.cleared`, stores one `Suggestion {text, session_id, generation, recorded_at}` per task in the bridge hub, and validates (blank, over 4 KiB, non-UTF-8 or NUL text -> 400 for the whole batch, stored state untouched).
- A suggestion clears on a new turn, an explicit clear, a session or launch-generation change and a new mod boot.
- `GET /api/tasks/{id}/bridge` gains `suggestion` (`null` or the object). It is `null` whenever the bridge is not live authority for the running launch.
- New `task.suggestion_changed` event `{session_id, available, recorded_at}`; the text is never in the event.
- Supervisor spawn surface 36 -> 37 (the mod ships in the supervisor binary).

## Non-Goals

- No web, TUI or macOS UI, and nothing is ever written to the PTY or the saved prompt. Web follow-up: ENG-1081 (agent suggestions in the empty composer). TUI keeps its provider terminal hints.
- No emulation for providers without a suggestion API (Codex etc.): they report `suggestion=null`.

## Impact

`internal/bridge`, `internal/bridgemod`, `internal/api`, `internal/model/event.go`, `internal/daemon/surface.go`.
