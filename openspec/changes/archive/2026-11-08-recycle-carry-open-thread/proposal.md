## Why

On 2026-11-08 the argus-paint coordinator answered the user's question ("is the slowness pure CPU load, or Argus in the render path?") and recycled seconds later. Its handoff_note covered the workers but omitted that exchange; the fresh session never knew about it and the user never saw the answer. A prompt-only rule would not have prevented it (the agent simply forgot), and a forced 1.5x recycle gives the agent no chance to write anything at all.

## What Changes

- **Part A (agent-written):** every place that tells a session to write a handoff or request a recycle now requires an "Open with the user" section (last unresolved user question/request, latest answer or thinking, any decision still owed, or "none"). The fresh session's seed prompt tells it to relay a non-empty section first.
- **Part B (daemon-extracted):** when building the recycled seed prompt, the daemon reads the PRIOR Claude transcript and adds a "Last exchange with the user before the reset" section (last genuine human message + the session's reply text after it), via one shared helper (`internal/lastexchange`) used by both seed builders. Fail-open, size-capped, local-only.
- `task_recycle` now builds its seed at restart time (after the idle wait) so the session's final reply is on disk when extracted.

## Capabilities

### Modified Capabilities

- `coordinator-context-management`: handoff/recycle instructions require "Open with the user"; hera seed carries the last exchange and a relay directive.
- `task-recycle`: plain-task seed carries the last exchange and relay directive; seed built at restart time.

## Non-Goals

- No REST-visible, MCP-schema-shape, or UI change: the prompt/description text and the seed prompt only. Frontend parity (TUI/web/macOS) is therefore not triggered.
- No Codex/opencode/other-backend transcript readers (omitted, fail-open).
- No new transcript locator: reuses `claudesession.ProjectDirIn` and `agent.ClaudeConfigDirForTask`.
- Supervisor spawn surface: untouched in behavior; only the digest was re-recorded because the hera SKILL.md (a manifest file) text changed.
