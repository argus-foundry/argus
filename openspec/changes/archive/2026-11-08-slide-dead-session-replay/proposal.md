## Why

#1165 made a replay emulator's cell budget a sliding window over the session log, but only for LIVE sessions. A dead session's replay stayed a single window (an 8 MB tail, trimmed to the cell budget): scrolling past its top was a plain clamp, so history older than that tail was unreachable. Dead sessions also truncated at the final alt-screen exit only within the tail read, never against the whole log.

## What Changes

- A dead session's replay now ends at its final alt-screen exit (`CSI ?1049l`) computed over the WHOLE log: `deadLogEnd` scans backward from EOF in overlapping blocks until it finds the last exit (an exit at offset 0, or none, means the log size), and caches the answer per (task, log size) since a dead log no longer changes.
- Every dead-session build (tail or slid) is bounded to `[0, limit)`: the tail read ends at `limit` instead of EOF, the `truncateAtFinalAltScreenExit` pass is skipped for log reads that already end there (it would otherwise cut at an EARLIER exit), and windows feed up to `limit`.
- The window-slide triggers no longer require a live session: a dead pane slides back near the window top (and extends an under-budget tail like a live one) and slides forward near a non-tail window's bottom; cache validity for dead panes now also consults the kick so a slide is not served from a stale window.

## Non-goals

- No behavior change for live sessions.
- Width resolution still goes through `replayEmuDims`.

## Impact

- `internal/tui/terminal/` (replaywindow.go, terminalpane.go).
