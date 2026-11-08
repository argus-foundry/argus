## Why

Claude Code brackets every screen update with `CSI ?2026h` … `CSI ?2026l` (DEC private mode 2026, synchronized output). The vendored emulator (`charmbracelet/x/vt`) ignores the mode, and the output-driven repaint pump can draw while a block is half-delivered (a block split across reads, or the frame cap firing inside it). The user then sees a torn intermediate state: a half-redrawn input box, or a status line from one frame over the body of another.

## What Changes

- The live feed path withholds an open 2026 block from the emulator until its `?2026l` arrives, so the last completed frame stays on screen. Tracking is our own zero-allocation byte matcher (`internal/tui/terminal/syncoutput.go`), handling sequences split across chunk boundaries and duplicate `h`/`h`/`l`; `x/vt` is not forked.
- Hard timeout `SyncOutputTimeout` = 150ms (matches Alacritty's synchronized-update timeout and our echoWindow) plus a 128KiB held-bytes cap, so a lost `?2026l` can never freeze a pane. Expiry is rate-limited in `ux.log` (one line per 10s per pane), never per frame.
- The repaint pump's per-subscription writer tracks the mode too and skips the wake for writes wholly inside an open block (the pane would hold them anyway); the opening and closing writes still wake. A pane that starts holding asks the pump for a one-shot timer wake at the hold's expiry, because a lost close produces no output to wake it.
- The hold state resets on every `ResetVT`, rebind (`SetSession` to a new session, fresh-bind), in-place emulator reset, and ring-wrap catch-up.

## Capabilities

### Modified Capabilities

- `terminal-rendering`: live follow-tail rendering honors synchronized output.

## Non-Goals

- **Replay and preview are unchanged.** They rebuild whole logs offline and paint once, so only the final state matters; `x/vt` ignores the unknown private mode, so the markers are inert there.
- Combined parameter forms (`CSI ?25;2026h`) are not recognized; Claude Code never emits them, and they degrade to today's behavior.
- No supervisor/daemon spawn-surface, REST, or MCP change: TUI-only.
- Frontend parity: xterm.js (web SPA) implements mode 2026 natively, so web needs nothing. The macOS app's SwiftTerm is checked in the PR description; neither client shares the Go pump.
