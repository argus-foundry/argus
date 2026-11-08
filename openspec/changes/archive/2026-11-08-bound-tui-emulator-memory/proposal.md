## Why

The TUI's pane emulators retain far more memory than their caps suggest. Measured on real session logs (`~/.argus/sessions/*.log`, read-only), x/vt's `uv.Cell` is 112 bytes, so a scrollback line costs `cells x 112 B` (~7 KB for a typical 62-cell line, ~22 KB for a full 200-column line).

- **`ClearScrollback` frees nothing.** x/vt's `Scrollback.Clear()` is `lines = lines[:0]`, so the backing array keeps every old line reachable. Every RIS-reset reuse of an emulator (replay spare, live pane, hera rebind) keeps its PEAK scrollback until the lines happen to be overwritten. Measured: retained heap is identical before and after `RIS + ClearScrollback`. This is the ~1 MB/h creep, and why idle spare replay slots hold a ~24 MB floor.
- **The replay "cap" is not a bound.** `replayScrollbackSize` is 50,000 lines, but a replay build feeds up to `replayRebuildMaxBytes` (64 MB). A dense 8 MB tail already yields ~480K cells (~54 MB); 64 MB is ~430 MB per emulator, and a pane holds up to 4.
- **Idle panes keep everything.** A pane not drawn for minutes keeps its published replay emulator and both spares.

Emulators cannot be Closed or dropped (drain goroutine keeps them reachable; Close races its Read), so release must shrink in place.

## What Changes

- Scrollback release primitive that zeroes retained lines before `Clear`, used wherever an emulator is RIS-reused (replay acquire, live reset).
- Replay emulators are bounded by a CELL budget (default 1,000,000 cells, ~112 MB); line cap kept as the secondary bound. `replayRebuildMaxBytes` lowered 64 MB -> 24 MB to limit the transient peak.
- The budget is a SLIDING WINDOW over the log, so deep history stays reachable: scrolling past the top of a budget-limited window rebuilds it further back, scrolling to the bottom of a back-shifted window slides it forward. Windows are fed forward in chunks with a line-to-byte checkpoint table and a content anchor so the cursor stays on the same line across a slide. Reuses BUG-E's trigger and the single-flight rebuild.
- Idle release: a pane not drawn for 5 minutes releases its spare slots and published replay emulator (rebuilt on return like a first view). The tick is driven by `App.onTick`, executed on the tview goroutine.
- uxlog on every trim, release and rebuild.

## Non-goals

- Dead sessions do not slide (final-frame truncation needs a whole-log scan); they get one budget-sized window.
- Live emulator (1,000 lines) is not released when idle: rebuilding it needs a full replay and risks a blank flash.
- No settings toggle: release only affects panes unseen for 5+ minutes.

## Impact

- `internal/tui/terminal/` (emumem.go, terminalpane.go), `internal/tui/app.go` (tick hook).
