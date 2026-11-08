## Why

The size-drift kick (kill + `--session-id` resume so history re-wraps at the new width) fires on a delay after a resize or Ctrl+Z zoom, and today it ignores two things the user cares about far more than a re-wrapped history:

- **Their place.** A deferred kick (busy → idle, or the 10 s–60 s Hera retry) can fire while the user is scrolled up reading. The resume re-renders the pane and they lose their place.
- **Their typed input.** The kill discards unsent composer text.

Aaron can live with history not re-wrapping; he cannot live with losing his place or his draft.

## What Changes

- **Kick right away** when the agent is idle, the pane is at the bottom, and the composer holds no unsent text (existing debounce unchanged).
- **Never kick while the pane is scrolled up.** The kick stays pending; once the pane is back at the bottom it re-arms through the existing `KickDebounce`. Covers the Tasks agent view and both Hera panes.
- **Never kick over unsent composer text.** Claude sessions with a live bridge report the draft; other sessions use a conservative TUI-local "typed since last submit" signal. A deferred kick re-arms when the draft clears.
- **Save-and-restore for Claude (safety net).** The argus-bridge mod reports the composer draft (`$.prompt.read`, on `prompt.change`) to the daemon. A resumed session's mod pulls any fresh pending draft once and puts it back with `$.prompt.fill`, only if the box is still empty. Covers a draft that appears between the gate check and the kill, and kicks forced by another surface.
- **Off switch.** A Settings toggle `bridge.keep_place` (default on) disables all of the above and restores today's behavior.
- uxlog line for every kick, deferral, save and restore outcome.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `argus-bridge`: the mod reports the composer draft; the daemon holds a fresh pending draft and hands it back exactly once to the next mod boot.
- `agent-view`: the size-drift kick gates on scroll position and unsent text, retrying when they clear.
- `hera-view`: the same gates for both Hera panes.
- `settings-view`: the `keep_place` toggle.

## Impact

- `internal/agent/rerender.go` (two new decisions), `internal/tui/app.go` (gate + Tasks-view retry), `internal/tui/hera/panes.go` (pane gate), `internal/bridge` + `internal/api` (draft event, pull endpoint), `internal/bridgemod/mod/argus-bridge` (reporter + restore), `internal/config` + `internal/db/config.go` + `internal/tui/settings_bridge.go` (setting).
- The mod ships in the supervisor binary: the spawn-surface digest re-records; an old supervisor keeps the old mod until restarted (the daemon tolerates a mod that never reports a draft). No RPC change, so `ProtocolVersion` is untouched.
- **Frontend parity.** Web/PWA and macOS share the daemon REST surface. The daemon's resize-kick handler (`Server.maybeKickRerender`) gets the bridge-draft gate. Scroll position is client-side in those apps, so scrolled-up gating there, and the non-bridge typed-since-submit signal for web, are named follow-ups (Non-Goals in design.md).
