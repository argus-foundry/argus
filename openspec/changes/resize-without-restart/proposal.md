## Why

When a Claude pane's width changes (Ctrl+Z fullscreen, window resize, rail rebind at a new width) the TUI runs the "size-drift kick": it kills Claude Code and runs `claude --resume` at the new width so history reprints re-wrapped. The user sees a distracting full replay on every layout change. Two research spikes (2026-11-08) showed the kick is no longer needed for correctness of the live region: Claude Code handles SIGWINCH itself (one `?2026` sync block that clears the viewport, then a re-layout at the new width, in both directions, with no scrollback writes). Older history keeps its authored width and is already shown correctly per segment via the `.sizes` replay (#1177), cropping when the pane is narrower.

## What Changes

- **Default behaviour change:** a width change resizes the PTY (SIGWINCH) and nothing else. No kill, no `--resume`, no replay.
- A **rebindable hotkey** (`agent.rerender`, default `shift+home`) runs today's kick on demand (kill+resume at the current pane width, keep-place and draft restore unchanged).
- When a pane's history is owed a re-render (existing drift predicate: width differs from the authored width by at least `RerenderMargin`), a **notice is pinned to the right side of the pane frame's top border** showing the hotkey, e.g. `width changed · shift+home re-render`. It clears after a re-render or when the width returns to the authored width. Applies to the Hera coordinator pane, the Hera agent pane, and the Tasks-tab agent view (all one `TerminalPane`).
- A **setting** `ui.auto_rerender_on_resize` (default `false`) restores today's automatic kick exactly. The auto-kick machinery stays, gated, not deleted. Removal is a named follow-up.
- A recycled or kick-restarted session **spawns at the pane's reported size** (fresh viewer report for its role, then live PTY size, then 24x80) rather than blindly inheriting the outgoing PTY's size.
- Accepted, named limitation: about 13 rows near each resize appear twice in scrollback at different widths; older history crops when the pane is narrower than its authored width. Paint-time soft-wrap was rejected (it mangles box tables/frames and forces a deep emulator-vs-display-row refactor).
- uxlog lines for notice shown/cleared, hotkey re-render, and the setting's state.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-view`: width change no longer restarts by default; owed-re-render notice and hotkey in the Tasks agent view; keep-place requirement re-scoped to every kick.
- `hera-view`: same behaviour for both Hera panes; zoom dwell and keep-place requirements re-scoped.
- `terminal-rendering`: in-pane status requirement gains the owed notice and its precedence.
- `keybindings`: new rebindable CtxAgent action `agent.rerender`.
- `config-management`: new `ui.auto_rerender_on_resize` setting.
- `agent-execution`: restarted sessions spawn at the reported pane size.

## Impact

- `internal/tui/app.go` (maybeKickRerender*/onTick gating, hotkey dispatch), `internal/tui/hera/panes.go` (maybeKickPaneRerender), `internal/tui/terminal/terminalpane.go` + `rerender.go` (notice), `internal/tui/keymap/actions.go`, `internal/tui/help.go`/`help_test.go`, `internal/config/config.go` + `internal/tui/settings_bridge.go` + `internal/apistore/store.go`, `internal/daemon/recycle.go`, README Reference, `context/knowledge/gotchas`.
- No wire change: `KickReq`/`RecycleReq` already carry rows/cols; `ProtocolVersion` stays 11; no supervisor spawn-surface change expected (verify the digest in tasks).
- Web SPA and macOS keep their daemon-side auto-kick in this change (named follow-up, see design Non-Goals).
