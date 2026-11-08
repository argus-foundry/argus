## Why

After Ctrl+Z shrinks a Hera pane (fullscreen to split), a scrolled-up coordinator shows its history garbled: word fragments down the left strip and a column of single letters on the right edge. It stays that way until the deferred kill+resume finally runs, and a constantly busy coordinator can wait minutes (seen: still pending after 4+ minutes, with retries backing off to 60s).

Root cause: the scroll-mode replay of a live session builds its emulator at the live PTY size, which a Ctrl+Z has already shrunk to the pane width. The history was re-emitted at the wider width the session started at, so a narrower emulator autowraps every long line. Reproduced offline by feeding the real session log through the replay path at 282 and 142 columns.

## What Changes

- The scroll replay of a live session emulates at the widest of the live PTY size and the session's initial PTY size, then clips to the pane. The pending view is readable (clipped, never fragmented) at no extra cost, since it is the same replay emulator built at a different width.
- A Ctrl+Z toggle in a Hera pane evaluates the size-drift kick after a 100ms dwell instead of the 300ms rail-traversal dwell, and restarts the busy-deferral backoff.
- The busy-deferral retry backoff caps at 15s (was 60s), so a coordinator that idles in short gaps is repaired at its next gap.
- No change to when a kick is allowed: a busy agent is still never killed to repair width.

## Non-Goals

- Forcing a kick on a busy coordinator. Resume restores the conversation but kills in-flight tool calls and background jobs.
- Exact rendering of history authored at several different widths (needs width markers in the session log). Separate follow-up change.
- Speeding up the replay rebuild itself (profiled: the cost is x/vt Write, about 1.5 MB/s).
- Frontend parity: the web and macOS clients render PTY bytes through xterm.js and SwiftTerm and do not use the TUI replay emulator or Hera pane kick scheduling; no REST-visible change. No follow-up needed.
