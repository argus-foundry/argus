## Why

A daemon restart marked live Hera coordinator sessions complete, and the finished-session check then stopped them for real. After a restart a client's liveness probe can wrongly report a still-running session dead. The exit-info query then answered success with an empty record, which reads as a clean exit. Workers survive this because they roll to in_review; coordinators fall into the ordinary clean-exit branch and become complete. Seen on three coordinators at once on 2026-11-07 and twice on 2026-11-08.

## What Changes

- The exit-info query answers "stream lost" instead of an empty record when it has no recorded exit for the reader, so no consumer can infer a clean exit from nothing.
- The session-status probe reports a failed supervisor query as an error instead of "not alive".
- The finished-session check never stops the session of a non-archived task holding a live coordinator or freelance binding, and logs the binding the first time it skips one.
- The supervisor stream surface version is bumped (15) and its digest re-recorded. The R/S ProtocolVersion (11) and the spawn surface are unchanged.

## Non-Goals

The exact cause of a probe that returns "not alive" for a live session is not proven; the failed-query case is one known path. The exit-info change is what makes any such false verdict harmless. No TUI, web or macOS surface change: all three already treat stream-lost as "status unchanged".
