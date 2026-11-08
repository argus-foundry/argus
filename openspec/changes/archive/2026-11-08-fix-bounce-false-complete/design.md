## Decisions

- **Fix point: the exit-info query, not the TUI or client.** Both the TUI and the daemon's supervisor relay call the same query, so one server-side change covers both and the TUI/daemon exit paths (already stream-lost-aware) need no edits. An empty record is the only value that reads as clean, so it must never be produced for "unknown".
- **Also surface failed supervisor probes as errors.** Handle lookup folds an RPC failure into nil, which the status handler published as not-alive. An error is already treated by clients as "daemon unreachable" (stream lost).
- **Reaper guard.** Coordinators and freelancers idle between messages by design, so idle plus a complete label is not evidence of being finished. Workers keep being reaped after acceptance. Archived tasks are still reaped: archiving is a deliberate human act. A binding lookup error fails closed.
- **Skew.** Stale supervisors keep serving the old query, so the stream surface is bumped; the resulting restart prompt is the price of closing the hole on the supervisor-to-daemon relay. A bounded probe error in an old supervisor is unaffected.
