## Decisions

- `needs_input` stays gated on `in_progress` (existing tests and the documented rail contract); the ticket says it retains its own detector and archive-clearing rules, so only liveness was added as a prerequisite.
- The event is derived by diffing the same `runtimeState` the REST DTO uses, from `idleWatcherTick`, so event and DTO cannot disagree. Latency is one watcher tick. The first tick after a daemon start publishes one event per live session (previous value unknown, treated as all-false).
- The `idle` wire field stays `omitempty`; `live` stays always-present.
