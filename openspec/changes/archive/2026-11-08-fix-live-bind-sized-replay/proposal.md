## Why

A Hera pane bound to a recycled coordinator showed the final assistant message painted over a stale copy of Claude's footer, with old glyphs showing through the word gaps. It happened on every rebind and cleared only when something forced Claude to repaint (Ctrl+Z fullscreen twice). No kick fired.

Root cause, reproduced offline from the preserved 789,627-byte log and its `.sizes` (`0 140 69` / `779946 88 48`): the live fresh-bind path (`renderLive` freshBind) builds the emulator at the session's initial size (140x69) and feeds the whole log tail in one write. #1177 gave the scroll-replay builds the per-segment `.sizes` replay but never the live bind. The last 9.7KB was authored at 88x48, so its relative cursor-up redraws land on the wrong rows of a 140x69 grid and two renders of the footer overlay about 14 rows apart. Feeding the same bytes per segment, or entirely at 88x48, is clean. A recycled session inherits the outgoing PTY size (`daemon/recycle.go`), which is why this session started at 140x69 while the pane is 90 wide.

## What Changes

- The live full-replay feed (fresh bind and ring-wrap rebuild) emulates each log segment at its authored size via the `.sizes` index, the same `feedReplayChunksSized` the scroll replay uses.
- `readLiveRebuildHistoryAt` also returns the log offset of the history's first byte so the index applies; the ring-only fallback reports no offset and stays a plain single-width feed.

## Non-Goals

- Why a recycled coordinator spawns at the outgoing PTY size rather than the pane size (noted, not changed).
- Any change to the size-drift kick.

## Impact

`internal/tui/terminal/terminalpane.go`. No REST/web/macOS surface change: web (xterm.js) and macOS (SwiftTerm) receive the live byte stream and never replay the log through this emulator, so they are unaffected.
